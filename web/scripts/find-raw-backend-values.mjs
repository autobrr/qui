#!/usr/bin/env node

/**
 * find-raw-backend-values.mjs
 *
 * Scans all TSX files for potential raw backend values rendered directly in JSX
 * without i18n translation via t(). This catches cases like:
 *   - {item.status}         → should be t(`namespace.statusLabels.${item.status}`)
 *   - {item.contentType}    → should be t(`namespace.contentTypeLabels.${item.contentType}`)
 *   - {item.linkMode}       → should be t(`namespace.linkModeLabels.${item.linkMode}`)
 *   - {item.phase}          → should be t(`namespace.phases.${item.phase}`)
 *   - {item.type}           → should be t(`namespace.typeLabels.${item.type}`)
 *   - {item.state}          → should be t(`namespace.stateLabels.${item.state}`)
 *   - {item.mode}           → should be t(`namespace.modeLabels.${item.mode}`)
 *
 * It also detects .toUpperCase() / .toLowerCase() calls on t() results,
 * which should use dedicated i18n keys instead.
 *
 * Usage: node scripts/find-raw-backend-values.mjs [--strict]
 *   --strict: exit with code 1 if any findings are reported
 */

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.resolve(__dirname, "../src");

// Properties that are typically backend enum values and should be localized
const SUSPECT_PROPERTIES = [
  "status",
  "contentType",
  "content_type",
  "linkMode",
  "link_mode",
  "phase",
  "state",
  "type",
  "mode",
  "trigger",
  "reason",
  "priority",
  "severity",
  "level",
  "kind",
  "category", // only when rendered standalone, not in templates
];

// Known safe patterns (these are intentionally raw, e.g. for CSS classes, data attributes, logic)
const SAFE_PATTERNS = [
  // className or variant usage
  /className/,
  /variant[=(]/,
  // Object key access for styling/logic
  /statusConfig\[/,
  /statusVariant/,
  /StatusVariant/,
  /statusColor/,
  /StatusColor/,
  // Known safe prop passing
  /value=\{/,
  /reason=\{/,
  /currentCategory=\{/,
  /currentFilter=\{/,
  /checkoutPaymentStatus=\{/,
  // Dynamic or unstructured text that can't be translated
  /event\.reason/,
  /disabledInfo\.reason/,
  /entry\.level/,
  /stream\.kind/,
  /fieldDef\?\.type/,
  // Conditional checks (not rendering)
  /===\s/,
  /!==\s/,
  /\?\s/,
  /&&\s/,
  /\|\|\s/,
  /switch\s*\(/,
  /case\s/,
  /if\s*\(/,
  // Array/object methods
  /\.filter\(/,
  /\.find\(/,
  /\.map\(/,
  /\.includes\(/,
  /\.indexOf\(/,
  /\.some\(/,
  /\.every\(/,
  /\.reduce\(/,
  /\.sort\(/,
  /\.forEach\(/,
  // Assignment / setter
  /\w+\s*=\s/,
  /set[A-Z]/,
  // API / data layer
  /mutate/,
  /queryKey/,
  /queryFn/,
  /onSuccess/,
  /onError/,
  /console\./,
  /toast\./,
  // Type annotations
  /:\s*(string|number|boolean|Record|Array)/,
  // Comments
  /^\s*\/\//,
  /^\s*\*/,
  // JSON key access for non-display purposes
  /\[["']/,
  // Event handlers
  /onClick/,
  /onChange/,
  /onSubmit/,
  // Template literal used as object key
  /\[`/,
  // Already wrapped in t()
  /\bt\s*\(/,
  /\bt\s*\(`/,
  // interpolation inside t()
  /t\([^)]*\$\{/,
];

// Files to skip entirely (test files, type definitions, etc.)
const SKIP_FILE_PATTERNS = [
  /\.test\.(tsx?|jsx?)$/,
  /\.spec\.(tsx?|jsx?)$/,
  /\.d\.ts$/,
  /__tests__/,
  /__mocks__/,
];

function getAllTsxFiles(dir) {
  const results = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...getAllTsxFiles(fullPath));
    } else if (/\.tsx$/.test(entry.name)) {
      if (!SKIP_FILE_PATTERNS.some((pat) => pat.test(fullPath))) {
        results.push(fullPath);
      }
    }
  }
  return results;
}

function isInJsxContext(line) {
  // Heuristic: line contains JSX-like patterns
  return (
    /[<>]/.test(line) ||
    /\{[^}]*\}/.test(line) ||
    /return\s*\(/.test(line) ||
    /^\s*</.test(line)
  );
}

function scanFile(filePath) {
  const content = fs.readFileSync(filePath, "utf8");
  const lines = content.split(/\r?\n/);
  const findings = [];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const lineNum = i + 1;

    // Check 1: Raw backend property rendered in JSX
    // Pattern: {something.suspectProperty} in a JSX context
    for (const prop of SUSPECT_PROPERTIES) {
      // Match patterns like {foo.status} or {foo?.status} or {item.status}
      // But NOT t(...status...) or className={...status...}
      const regex = new RegExp(
        `\\{[^}]*\\b\\w+(?:\\.|\\?\\.)${prop}\\b[^}]*\\}`,
        "g"
      );
      const matches = line.matchAll(regex);

      for (const match of matches) {
        const matchStr = match[0];

        // Skip if any safe pattern matches
        if (SAFE_PATTERNS.some((pat) => pat.test(line))) continue;

        // Extra check: is this likely a JSX render context?
        // Look for surrounding JSX or if this is inside a JSX return
        if (!isInJsxContext(line)) continue;

        // Check if the expression is just the property (direct render)
        // vs being part of a larger expression
        const simpleRender = new RegExp(
          `\\{\\s*\\w+(?:\\.|\\?\\.)${prop}\\s*\\}|>\\s*\\{\\s*\\w+(?:\\.|\\?\\.)${prop}\\s*\\}\\s*<`
        );
        if (simpleRender.test(line)) {
          findings.push({
            line: lineNum,
            type: "RAW_BACKEND_VALUE",
            property: prop,
            code: line.trim(),
            suggestion: `Use t(\`namespace.${prop}Labels.\${variable.${prop}}\`, variable.${prop}) instead`,
          });
        }
      }
    }

    // Check 2: .toUpperCase() on t() results
    if (/\bt\s*\([^)]*\)\s*\.toUpperCase\s*\(\s*\)/.test(line)) {
      findings.push({
        line: lineNum,
        type: "RUNTIME_CASE_TRANSFORM",
        property: null,
        code: line.trim(),
        suggestion:
          "Use a dedicated i18n key (e.g. statusLabelsUpper) instead of runtime .toUpperCase()/.toLowerCase()",
      });
    }

    // Check 3: String interpolation with backend values without t()
    // Pattern: `${foo.status}` not inside t()
    for (const prop of SUSPECT_PROPERTIES) {
      const interpolationRegex = new RegExp(
        `\`[^\\x60]*\\$\\{\\w+\\.${prop}\\}[^\\x60]*\``,
        "g"
      );
      if (interpolationRegex.test(line)) {
        // Make sure it's not inside a t() call
        if (!/\bt\s*\(/.test(line) && isInJsxContext(line)) {
          if (SAFE_PATTERNS.some((pat) => pat.test(line))) continue;
          findings.push({
            line: lineNum,
            type: "RAW_INTERPOLATION",
            property: prop,
            code: line.trim(),
            suggestion: `Backend value '${prop}' interpolated without t() - consider localizing`,
          });
        }
      }
    }
  }

  return findings;
}

// Main
const strict = process.argv.includes("--strict");
const files = getAllTsxFiles(srcDir);
let totalFindings = 0;

console.log(`\n🔍 Scanning ${files.length} TSX files for raw backend values...\n`);

for (const file of files) {
  const findings = scanFile(file);
  if (findings.length === 0) continue;

  const relPath = path.relative(path.resolve(__dirname, ".."), file);
  console.log(`\n📄 ${relPath}`);

  for (const f of findings) {
    totalFindings++;
    const icon =
      f.type === "RAW_BACKEND_VALUE"
        ? "⚠️"
        : f.type === "RUNTIME_CASE_TRANSFORM"
          ? "🔤"
          : "📝";
    console.log(`  ${icon} Line ${f.line}: [${f.type}]${f.property ? ` (${f.property})` : ""}`);
    console.log(`     Code: ${f.code.substring(0, 120)}${f.code.length > 120 ? "..." : ""}`);
    console.log(`     Fix:  ${f.suggestion}`);
  }
}

console.log(`\n${"=".repeat(60)}`);
if (totalFindings === 0) {
  console.log("✅ No raw backend values found in JSX! All clean.");
} else {
  console.log(
    `⚠️  Found ${totalFindings} potential raw backend value(s) in JSX.`
  );
  console.log(
    "   Review each finding - some may be intentional (e.g., technical IDs)."
  );
}
console.log(`${"=".repeat(60)}\n`);

if (strict && totalFindings > 0) {
  process.exit(1);
}
