import fs from "node:fs"
import path from "node:path"
import ts from "typescript"

const webRoot = path.resolve(import.meta.dirname, "..")
const srcRoot = path.join(webRoot, "src")

const interestingAttributeNames = new Set([
  "title",
  "placeholder",
  "label",
  "description",
  "alt",
  "aria-label",
  "aria-description",
])

const interestingPropertyNames = new Set([
  "label",
  "title",
  "description",
  "placeholder",
  "emptyText",
  "helperText",
  "tooltip",
  "message",
  "text",
  "heading",
  "subheading",
  "buttonLabel",
  "ctaLabel",
  "confirmText",
  "cancelText",
])

const interestingVariableNames = new Set([
  "title",
  "subtitle",
  "description",
  "label",
  "placeholder",
  "emptyText",
  "helperText",
  "tooltip",
  "message",
  "heading",
  "subheading",
  "buttonLabel",
  "ctaLabel",
  "confirmText",
  "cancelText",
  "successMessage",
  "errorMessage",
])

// Suffixes that indicate a variable holds user-facing text.
// Matched case-insensitively against the end of compound variable names
// so `connectionStatusTooltip` matches via "Tooltip".
const interestingVariableSuffixes = [
  "Tooltip", "Label", "AriaLabel", "Summary",
  "Message", "Title", "Description", "Placeholder",
  "Heading", "Text", "Caption",
]

// Functions whose return values are user-facing text.
const formatFunctionPattern = /^(?:format|get\w*(?:Label|Summary|Text|Tooltip|Status|Display|Caption))/

const ignoredJsxTags = new Set([
  "code",
  "pre",
  "style",
  "script",
])

function normalizeText(text) {
  return text.replace(/\s+/g, " ").trim()
}

// Check whether an AST node is "transparent" for context detection --
// i.e. the string's UI purpose comes from whatever wraps this node.
function isTransparentExpression(node) {
  return ts.isConditionalExpression(node)
    || ts.isParenthesizedExpression(node)
    || ts.isTemplateSpan(node)
    || ts.isTemplateExpression(node)
}

function shouldTrackText(text, relaxed = false) {
  if (!text) return false
  // URL defaults / placeholders
  if (/^https?:\/\//.test(text)) return false
  // Duration abbreviation compounds: "<1s", "5m 30s", "2h 15m", "m s", "h m", "< 1m"
  if (/^[<>]?\s*\d*\s*[dhms](?:\s+\d*\s*[dhms])?$/.test(text)) return false
  // Size format fallbacks: "0 B", "10 KB", etc.
  if (/^\d+\s+[KMGT]?i?B$/.test(text)) return false
  // JSON / code example patterns in placeholders
  if (/^\{[\s\S]*:[\s\S]*\}$/.test(text)) return false
  if (!/[A-Za-z]/.test(text)) return false
  // Dotted identifiers like "foo.bar.baz"
  if (/^[a-z0-9-]+(?:\.[a-z0-9_-]+)+$/i.test(text)) return false
  // CSS/UI variant names and common non-UI return values (before the relaxed
  // lowercase check so these are always filtered regardless of context)
  if (/^(?:default|secondary|destructive|outline|ghost|link|muted|accent|primary)$/.test(text)) return false
  // Units, product names, technical terms, and other non-translatable tokens.
  // Must be checked before the ALL_CAPS / lowercase-word gates below, because
  // those gates return true in relaxed mode for short caps or longer lowercase
  // words, which would incorrectly flag entries like "RSS" or "autobrr".
  if (/^(?:[KMGT]?i?B(?:\/s)?|B\/s|Mbps|[dhms]|ms|lt|qBit|API v|IPv4|IPv6|Napster|Swizzin|RSS|README|autobrr|qui-premium|cross-seed|<redacted>|libtorrent\s.*\.x|cross-seed\/|\.cross|\/\s*[dhms]|\*\*\*masked\*\*\*|\/\/\*\*\*masked\*\*\*)$/u.test(text)) return false
  // ALL_CAPS constants -- but in relaxed mode, flag short UI words like "ALL"
  if (/^[A-Z0-9_]+$/.test(text)) {
    return relaxed && text.length >= 3
  }
  // Single lowercase words -- in relaxed mode, flag longer ones like "action"
  if (/^[a-z0-9-]+$/.test(text)) {
    return relaxed && text.length >= 5
  }
  return true
}

function getNodeLineAndColumn(sourceFile, node) {
  const { line, character } = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
  return { line: line + 1, column: character + 1 }
}

function getJsxTagName(node) {
  if (ts.isJsxElement(node)) {
    return node.openingElement.tagName.getText()
  }
  if (ts.isJsxSelfClosingElement(node)) {
    return node.tagName.getText()
  }
  return null
}

function hasIgnoredJsxAncestor(node) {
  let current = node.parent
  while (current) {
    const tagName = getJsxTagName(current)
    if (tagName && ignoredJsxTags.has(tagName)) {
      return true
    }
    current = current.parent
  }
  return false
}

function isTranslationCall(node) {
  if (!ts.isCallExpression(node)) return false

  const { expression } = node
  if (ts.isIdentifier(expression)) {
    return expression.text === "t"
  }

  return ts.isPropertyAccessExpression(expression)
    && expression.name.text === "t"
}

function hasTranslationCallAncestor(node) {
  let current = node.parent
  while (current) {
    if (isTranslationCall(current)) {
      return true
    }
    current = current.parent
  }
  return false
}

// Walk up through transparent expressions and JsxExpression wrappers
// to find a JSX element/fragment parent -- meaning the string renders as
// child content of a JSX element.
function isJsxChildExpressionString(node) {
  let current = node.parent
  while (current) {
    if (ts.isJsxExpression(current)) {
      const p = current.parent
      return ts.isJsxElement(p) || ts.isJsxFragment(p)
    }
    if (isTransparentExpression(current)) {
      current = current.parent
      continue
    }
    return false
  }
  return false
}

// Walk up through transparent expressions and JsxExpression to find a
// JSX attribute with an interesting name.  Catches template literals
// inside `aria-label={`Send test to ${name}`}`.
function isInterestingJsxAttributeString(node) {
  let current = node.parent
  while (current) {
    if (ts.isJsxAttribute(current)) {
      return interestingAttributeNames.has(current.name.text)
    }
    if (isTransparentExpression(current) || ts.isJsxExpression(current)) {
      current = current.parent
      continue
    }
    return false
  }
  return false
}

function isInterestingPropertyString(node, sourceFile) {
  if (!/\.[jt]sx$/i.test(sourceFile.fileName)) return false
  if (!ts.isPropertyAssignment(node.parent)) return false
  if (!ts.isIdentifier(node.parent.name) && !ts.isStringLiteral(node.parent.name)) return false

  const propertyName = ts.isIdentifier(node.parent.name)
    ? node.parent.name.text
    : node.parent.name.text

  return interestingPropertyNames.has(propertyName)
}

// Walk up through transparent expressions to find a variable declaration,
// then check if the variable name is interesting (exact match or suffix).
function isInterestingVariableString(node, sourceFile) {
  if (!/\.[jt]sx$/i.test(sourceFile.fileName)) return false

  let current = node.parent
  while (current) {
    if (ts.isVariableDeclaration(current)) {
      if (!ts.isIdentifier(current.name)) return false
      const name = current.name.text
      if (interestingVariableNames.has(name)) return true
      return interestingVariableSuffixes.some((suffix) =>
        name.length > suffix.length && name.endsWith(suffix)
      )
    }
    if (isTransparentExpression(current)) {
      current = current.parent
      continue
    }
    return false
  }
  return false
}

function isToastCallString(node) {
  if (!ts.isCallExpression(node.parent)) return false
  const { expression } = node.parent

  return ts.isPropertyAccessExpression(expression)
    && ts.isIdentifier(expression.expression)
    && expression.expression.text === "toast"
}

// Detect string literals returned from functions whose names suggest they
// produce user-facing text (formatAction, getProxyTypeLabel, etc.).
function isFormatFunctionReturn(node, sourceFile) {
  if (!/\.[jt]sx$/i.test(sourceFile.fileName)) return false

  // Walk up to find a return statement (through transparent expressions).
  let current = node.parent
  while (current) {
    if (ts.isReturnStatement(current)) break
    if (isTransparentExpression(current)) {
      current = current.parent
      continue
    }
    return false
  }
  if (!current) return false

  // Walk up from the return to find the enclosing named function.
  current = current.parent
  while (current) {
    if (ts.isFunctionDeclaration(current) && current.name) {
      return formatFunctionPattern.test(current.name.text)
    }
    if (ts.isArrowFunction(current) || ts.isFunctionExpression(current)) {
      const p = current.parent
      if (ts.isVariableDeclaration(p) && ts.isIdentifier(p.name)) {
        return formatFunctionPattern.test(p.name.text)
      }
      return false
    }
    // Walk through blocks, switch/case structure, and if-else chains.
    if (
      ts.isBlock(current)
      || ts.isCaseClause(current)
      || ts.isDefaultClause(current)
      || ts.isSwitchStatement(current)
      || ts.isIfStatement(current)
      || ts.isCaseBlock(current)
    ) {
      current = current.parent
      continue
    }
    return false
  }
  return false
}

function readTemplateText(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text
  }

  if (ts.isTemplateExpression(node)) {
    const parts = [node.head.text]
    for (const span of node.templateSpans) {
      parts.push(span.literal.text)
    }
    return normalizeText(parts.join(" "))
  }

  return ""
}

function addMatch(matches, seen, sourceFile, node, text, kind) {
  const normalized = normalizeText(text)
  // Use relaxed filtering for contexts where we're already confident the
  // string is user-facing (JSX content, known attribute/property names,
  // format function returns).
  const relaxed = kind === "jsx-text"
    || kind === "jsx-attribute"
    || kind === "object-property"
    || kind === "format-return"
  if (!shouldTrackText(normalized, relaxed)) return
  if (hasIgnoredJsxAncestor(node)) return

  const { line, column } = getNodeLineAndColumn(sourceFile, node)
  const key = `${line}:${column}:${normalized}:${kind}`
  if (seen.has(key)) return
  seen.add(key)

  matches.push({
    file: sourceFile.fileName,
    line,
    column,
    text: normalized,
    kind,
  })
}

export function findHardcodedStringsInSource(source, filePath = "source.tsx") {
  const sourceFile = ts.createSourceFile(filePath, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const matches = []
  const seen = new Set()

  function visit(node) {
    if (ts.isJsxText(node)) {
      addMatch(matches, seen, sourceFile, node, node.text, "jsx-text")
    }

    if (
      ts.isStringLiteral(node)
      || ts.isNoSubstitutionTemplateLiteral(node)
      || ts.isTemplateExpression(node)
    ) {
      if (!hasTranslationCallAncestor(node)) {
        const text = readTemplateText(node)

        if (isInterestingJsxAttributeString(node)) {
          addMatch(matches, seen, sourceFile, node, text, "jsx-attribute")
        } else if (isInterestingPropertyString(node, sourceFile)) {
          addMatch(matches, seen, sourceFile, node, text, "object-property")
        } else if (isInterestingVariableString(node, sourceFile)) {
          addMatch(matches, seen, sourceFile, node, text, "variable")
        } else if (isToastCallString(node)) {
          addMatch(matches, seen, sourceFile, node, text, "toast-call")
        } else if (isFormatFunctionReturn(node, sourceFile)) {
          addMatch(matches, seen, sourceFile, node, text, "format-return")
        } else if (isJsxChildExpressionString(node)) {
          addMatch(matches, seen, sourceFile, node, text, "jsx-expression")
        }
      }
    }

    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return matches
}

function shouldScanFile(relativePath) {
  if (!/\.(ts|tsx|js|jsx)$/.test(relativePath)) return false
  if (!relativePath.startsWith("src/")) return false
  if (relativePath.startsWith("src/i18n/")) return false
  if (relativePath.endsWith(".test.tsx") || relativePath.endsWith(".test.ts") || relativePath.endsWith(".test.jsx") || relativePath.endsWith(".test.js")) return false
  if (relativePath.includes("/__tests__/")) return false
  if (relativePath.endsWith("routeTree.gen.ts")) return false
  return true
}

function walkFiles(rootDir) {
  const files = []

  function visit(dirPath) {
    for (const entry of fs.readdirSync(dirPath, { withFileTypes: true })) {
      const fullPath = path.join(dirPath, entry.name)
      if (entry.isDirectory()) {
        visit(fullPath)
        continue
      }

      const relativePath = path.relative(webRoot, fullPath)
      if (shouldScanFile(relativePath)) {
        files.push(fullPath)
      }
    }
  }

  visit(rootDir)
  return files.sort()
}

export function findHardcodedStringsInFiles(files) {
  return files.flatMap((filePath) => {
    const source = fs.readFileSync(filePath, "utf8")
    return findHardcodedStringsInSource(source, filePath)
  })
}

function groupMatchesByFile(matches) {
  const grouped = new Map()

  for (const match of matches) {
    const fileMatches = grouped.get(match.file) ?? []
    fileMatches.push(match)
    grouped.set(match.file, fileMatches)
  }

  return grouped
}

function printMatches(matches) {
  const grouped = groupMatchesByFile(matches)

  for (const [filePath, fileMatches] of grouped) {
    const relativePath = path.relative(webRoot, filePath)
    console.log(relativePath)
    for (const match of fileMatches) {
      console.log(`  ${match.line}:${match.column}  [${match.kind}] ${JSON.stringify(match.text)}`)
    }
  }

  console.log(`\nFound ${matches.length} hardcoded string matches across ${grouped.size} files.`)
}

if (process.argv[1] === import.meta.filename) {
  const files = walkFiles(srcRoot)
  const matches = findHardcodedStringsInFiles(files)

  if (matches.length === 0) {
    console.log("No hardcoded i18n literals found.")
    process.exit(0)
  }

  printMatches(matches)
  process.exit(1)
}
