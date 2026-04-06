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

const ignoredJsxTags = new Set([
  "code",
  "pre",
  "style",
  "script",
])

function normalizeText(text) {
  return text.replace(/\s+/g, " ").trim()
}

function shouldTrackText(text) {
  if (!text) return false
  if (!/[A-Za-z]/.test(text)) return false
  if (/^[a-z0-9-]+(?:\.[a-z0-9_-]+)+$/i.test(text)) return false
  if (/^[A-Z0-9_]+$/.test(text)) return false
  if (/^[a-z0-9-]+$/.test(text)) return false
  if (/^(?:[KMGT]?i?B(?:\/s)?|B\/s|[dhms]|ms|lt|qBit|API v|IPv4|IPv6|Napster|Swizzin|\*\*\*masked\*\*\*|\/\/\*\*\*masked\*\*\*)$/u.test(text)) return false
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
  return ts.isJsxExpression(node.parent)
    && (ts.isJsxElement(node.parent.parent) || ts.isJsxFragment(node.parent.parent))
}

function isInterestingJsxAttributeString(node) {
  return ts.isJsxAttribute(node.parent)
    && interestingAttributeNames.has(node.parent.name.text)
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

function isInterestingVariableString(node, sourceFile) {
  if (!/\.[jt]sx$/i.test(sourceFile.fileName)) return false
  if (!ts.isVariableDeclaration(node.parent)) return false
  if (!ts.isIdentifier(node.parent.name)) return false

  return interestingVariableNames.has(node.parent.name.text)
}

function isToastCallString(node) {
  if (!ts.isCallExpression(node.parent)) return false
  const { expression } = node.parent

  return ts.isPropertyAccessExpression(expression)
    && ts.isIdentifier(expression.expression)
    && expression.expression.text === "toast"
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
  if (!shouldTrackText(normalized)) return
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
