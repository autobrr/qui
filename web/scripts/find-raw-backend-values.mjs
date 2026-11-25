#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.resolve(__dirname, "../src");

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
  "category",
];

const SAFE_PATTERNS = [
  /className/,
  /variant[=(]/,
  /statusConfig\[/,
  /statusVariant/,
  /StatusVariant/,
  /statusColor/,
  /StatusColor/,
  /value=\{/,
  /reason=\{/,
  /currentCategory=\{/,
  /currentFilter=\{/,
  /checkoutPaymentStatus=\{/,
  /event\.reason/,
  /disabledInfo\.reason/,
  /entry\.level/,
  /stream\.kind/,
  /fieldDef\?\.type/,
  /===\s/,
  /!==\s/,
  /\?\s/,
  /&&\s/,
  /\|\|\s/,
  /switch\s*\(/,
  /case\s/,
  /if\s*\(/,
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
  /\w+\s*=\s/,
  /set[A-Z]/,
  /mutate/,
  /queryKey/,
  /queryFn/,
  /onSuccess/,
  /onError/,
  /console\./,
  /toast\./,
  /:\s*(string|number|boolean|Record|Array)/,
  /^\s*\/\//,
  /^\s*\*/,
  /\[["']/,
  /onClick/,
  /onChange/,
  /onSubmit/,
  /\[`/,
  /\bt\s*\(/,
  /\bt\s*\(`/,
  /t\([^)]*\$\{/,
];

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
  return (
    /[<>]/.test(line) ||
    /\{[^}]*\}/.test(line) ||
    /return\s*\(/.test(line) ||
    /^\s*</.test(line)
  );
}

function withoutKeyProps(line) {
  return line.replace(/(^|\s)key=\{(?:[^{}]|\{[^{}]*\})*\}/g, (match, space, offset) => {
    let depth = 0;
    let quote = "";
    let lastOpen = -1;
    let lastClose = -1;
    for (let i = 0; i < offset; i++) {
      const c = line[i];
      if (quote) {
        if (c === quote) quote = "";
      } else if (depth === 0 && (c === '"' || c === "'") && line[i - 1] === "=") quote = c;
      else if (c === "{") depth++;
      else if (c === "}") depth = Math.max(0, depth - 1);
      else if (depth === 0 && c === "<") lastOpen = i;
      else if (depth === 0 && c === ">") lastClose = i;
    }
    return lastOpen > lastClose ? space : match;
  });
}

function scanFile(filePath) {
  const content = fs.readFileSync(filePath, "utf8");
  const lines = content.split(/\r?\n/);
  const findings = [];

  for (let i = 0; i < lines.length; i++) {
    const line = withoutKeyProps(lines[i]);
    const lineNum = i + 1;

    for (const prop of SUSPECT_PROPERTIES) {
      const regex = new RegExp(
        `\\{[^}]*\\b\\w+(?:\\.|\\?\\.)${prop}\\b[^}]*\\}`
      );
      if (!regex.test(line)) continue;

      if (SAFE_PATTERNS.some((pat) => pat.test(line))) continue;

      if (!isInJsxContext(line)) continue;

      const simpleRender = new RegExp(
        `\\{\\s*\\w+(?:\\.|\\?\\.)${prop}\\s*\\}|>\\s*\\{\\s*\\w+(?:\\.|\\?\\.)${prop}\\s*\\}\\s*<`
      );
      if (simpleRender.test(line)) {
        findings.push({
          line: lineNum,
          type: "RAW_BACKEND_VALUE",
          property: prop,
          code: lines[i].trim(),
          suggestion: `Use t(\`namespace.${prop}Labels.\${variable.${prop}}\`, variable.${prop}) instead`,
        });
      }
    }

    if (/\bt\s*\([^)]*\)\s*\.toUpperCase\s*\(\s*\)/.test(line)) {
      findings.push({
        line: lineNum,
        type: "RUNTIME_CASE_TRANSFORM",
        property: null,
        code: lines[i].trim(),
        suggestion:
          "Use a dedicated i18n key (e.g. statusLabelsUpper) instead of runtime .toUpperCase()/.toLowerCase()",
      });
    }

    for (const prop of SUSPECT_PROPERTIES) {
      const interpolationRegex = new RegExp(
        `\`[^\\x60]*\\$\\{\\w+\\.${prop}\\}[^\\x60]*\``,
        "g"
      );
      if (interpolationRegex.test(line)) {
        if (!/\bt\s*\(/.test(line) && isInJsxContext(line)) {
          if (SAFE_PATTERNS.some((pat) => pat.test(line))) continue;
          findings.push({
            line: lineNum,
            type: "RAW_INTERPOLATION",
            property: prop,
            code: lines[i].trim(),
            suggestion: `Backend value '${prop}' interpolated without t() - consider localizing`,
          });
        }
      }
    }
  }

  return findings;
}

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
