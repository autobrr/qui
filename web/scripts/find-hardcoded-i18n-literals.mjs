import fs from "node:fs"
import path from "node:path"
import ts from "typescript"

const scriptPath = fs.realpathSync(import.meta.filename)
const webRoot = path.resolve(path.dirname(scriptPath), "..")
const srcRoot = path.join(webRoot, "src")

const interestingAttributeNames = new Set([
  "title",
  "placeholder",
  "label",
  "description",
  "alt",
  "unit",
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
  "reason",
])

const fallbackOnlyFile = "src/components/query-builder/constants.ts"

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

const interestingVariableSuffixes = [
  "Tooltip", "Label", "AriaLabel", "Summary",
  "Message", "Title", "Description", "Placeholder",
  "Heading", "Text", "Caption",
]

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

function isTransparentExpression(node) {
  return ts.isConditionalExpression(node)
    || ts.isParenthesizedExpression(node)
    || ts.isTemplateSpan(node)
    || ts.isTemplateExpression(node)
}

function shouldTrackText(text, relaxed = false) {
  if (!text) return false
  if (/^https?:\/\//.test(text)) return false
  if (/^[<>]?\s*\d*\s*[dhms](?:\s+\d*\s*[dhms])?$/.test(text)) return false
  if (/^\{[\s\S]*:[\s\S]*\}$/.test(text)) return false
  if (!/[A-Za-z]/.test(text)) return false
  if (/^[a-z0-9-]+(?:\.[a-z0-9_-]+)+$/i.test(text)) return false
  if (/^(?:default|secondary|destructive|outline|ghost|link|muted|accent|primary)$/.test(text)) return false
  if (/^(?:[dhms]|ms|lt|qBit|API v|IPv4|IPv6|Napster|Swizzin|RSS|README|autobrr|qui-premium|qui-patron|cross-seed|<redacted>|libtorrent\s.*\.x|cross-seed\/|\.cross|\/\s*[dhms]|\*\*\*masked\*\*\*|\/\/\*\*\*masked\*\*\*)$/u.test(text)) return false
  if (/^[A-Z0-9_]+$/.test(text)) {
    return relaxed && text.length >= 3
  }
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

function isFallbackOnlyFile(sourceFile) {
  return sourceFile.fileName.replaceAll("\\", "/").endsWith(fallbackOnlyFile)
}

function isInterestingPropertyString(node, sourceFile) {
  if (!/\.[jt]sx?$/i.test(sourceFile.fileName)) return false
  if (!ts.isPropertyAssignment(node.parent)) return false
  if (!ts.isIdentifier(node.parent.name) && !ts.isStringLiteral(node.parent.name)) return false

  const propertyName = ts.isIdentifier(node.parent.name)
    ? node.parent.name.text
    : node.parent.name.text

  return interestingPropertyNames.has(propertyName) && !isFallbackOnlyFile(sourceFile)
}

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

function isFormatFunctionReturn(node, sourceFile) {
  if (!/\.[jt]sx$/i.test(sourceFile.fileName)) return false

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

export function walkFiles(rootDir) {
  const files = []

  function visit(dirPath) {
    for (const entry of fs.readdirSync(dirPath, { withFileTypes: true })) {
      const fullPath = path.join(dirPath, entry.name)
      if (entry.isDirectory()) {
        visit(fullPath)
        continue
      }

      const relativePath = path.relative(webRoot, fullPath).replaceAll(path.sep, "/")
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

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const files = walkFiles(srcRoot)
  const matches = findHardcodedStringsInFiles(files)

  if (matches.length === 0) {
    console.log("No hardcoded i18n literals found.")
    process.exit(0)
  }

  printMatches(matches)
  process.exit(1)
}
