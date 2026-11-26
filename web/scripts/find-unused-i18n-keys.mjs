import fs from "node:fs"
import path from "node:path"
import ts from "typescript"

import { walkFiles } from "./find-hardcoded-i18n-literals.mjs"

const scriptPath = fs.realpathSync(import.meta.filename)
const webRoot = path.resolve(path.dirname(scriptPath), "..")
const srcRoot = path.join(webRoot, "src")
const englishLocaleRoot = path.join(srcRoot, "i18n", "locales", "en")

const pluralSuffixPattern = /_(?:ordinal_)?(?:zero|one|two|few|many|other)$/

const keyPathPattern = /^[A-Za-z0-9_$-]+(?::[A-Za-z0-9_$-]+)?(?:\.[A-Za-z0-9_$-]+)*$/

function collectLeafKeys(node, prefix, leaves) {
  for (const [segment, value] of Object.entries(node)) {
    const keyPath = prefix ? `${prefix}.${segment}` : segment

    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      collectLeafKeys(value, keyPath, leaves)
      continue
    }

    leaves.push(keyPath)
  }
}

export function collectLocaleKeys(namespaceBundles) {
  const keys = []

  for (const [namespace, bundle] of Object.entries(namespaceBundles)) {
    const leaves = []
    collectLeafKeys(bundle, "", leaves)

    const seen = new Set()
    for (const leaf of leaves) {
      const baseKey = leaf.replace(pluralSuffixPattern, "")
      if (seen.has(baseKey)) {
        continue
      }

      seen.add(baseKey)
      keys.push({ namespace, key: baseKey })
    }
  }

  return keys
}

export function collectKeyReferencesFromSource(source, fileName) {
  const sourceFile = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true)
  const literals = new Set()
  const prefixes = new Set()

  function addLiteral(value) {
    if (keyPathPattern.test(value)) {
      literals.add(value)
    }
  }

  function addPrefix(headText) {
    const lastDotIndex = headText.lastIndexOf(".")
    if (lastDotIndex === -1) {
      return
    }

    const prefix = headText.slice(0, lastDotIndex)
    if (prefix && keyPathPattern.test(prefix)) {
      prefixes.add(prefix)
    }
  }

  function addVariableHeadTemplate(node) {
    const [firstSpan, ...otherSpans] = node.templateSpans
    const headValue = ts.isIdentifier(firstSpan.expression) && resolveStringConst(firstSpan.expression)
    if (!headValue) {
      return
    }

    const staticHead = headValue + firstSpan.literal.text
    if (otherSpans.length === 0) {
      addLiteral(staticHead)
      return
    }

    if (otherSpans.length === 1 && staticHead.endsWith(".") && otherSpans[0].literal.text === "") {
      for (const segment of segmentLiteralsInEnclosingFunction(node)) {
        addLiteral(staticHead + segment)
      }
      return
    }

    addPrefix(staticHead)
  }

  function visit(node) {
    if (ts.isStringLiteralLike(node)) {
      addLiteral(node.text)
    } else if (ts.isTemplateExpression(node)) {
      if (node.head.text) {
        addPrefix(node.head.text)
      } else {
        addVariableHeadTemplate(node)
      }
    }

    ts.forEachChild(node, visit)
  }

  visit(sourceFile)

  return { literals, prefixes }
}

function resolveStringConst(identifier) {
  for (let scope = identifier.parent; scope; scope = scope.parent) {
    for (const statement of scope.statements ?? []) {
      if (!ts.isVariableStatement(statement) || !(statement.declarationList.flags & ts.NodeFlags.Const)) {
        continue
      }

      for (const declaration of statement.declarationList.declarations) {
        if (ts.isIdentifier(declaration.name) && declaration.name.text === identifier.text) {
          return declaration.initializer && ts.isStringLiteralLike(declaration.initializer) ? declaration.initializer.text : null
        }
      }
    }
  }

  return null
}

function segmentLiteralsInEnclosingFunction(node) {
  let scope = node.parent
  while (scope && !ts.isFunctionLike(scope) && !ts.isSourceFile(scope)) {
    scope = scope.parent
  }

  const segments = new Set()
  function collect(child) {
    if (ts.isStringLiteralLike(child) && /^[A-Za-z0-9_$-]+$/.test(child.text)) {
      segments.add(child.text)
    }
    ts.forEachChild(child, collect)
  }
  collect(scope)

  return segments
}

function isReachable(namespace, key, references) {
  const qualifiedKey = `${namespace}:${key}`
  if (references.literals.has(key) || references.literals.has(qualifiedKey)) {
    return true
  }

  for (const prefix of references.prefixes) {
    const candidate = prefix.includes(":") ? qualifiedKey : key
    if (candidate === prefix || candidate.startsWith(`${prefix}.`)) {
      return true
    }
  }

  return false
}

export function findUnusedKeys(localeKeys, references) {
  return localeKeys
    .filter(({ namespace, key }) => !isReachable(namespace, key, references))
    .map(({ namespace, key }) => `${namespace}:${key}`)
    .sort()
}

export function mergeReferences(referenceSets) {
  const merged = { literals: new Set(), prefixes: new Set() }

  for (const references of referenceSets) {
    for (const literal of references.literals) merged.literals.add(literal)
    for (const prefix of references.prefixes) merged.prefixes.add(prefix)
  }

  return merged
}

function loadEnglishBundles() {
  const bundles = {}

  for (const entry of fs.readdirSync(englishLocaleRoot)) {
    if (!entry.endsWith(".json")) {
      continue
    }

    const namespace = entry.slice(0, -".json".length)
    bundles[namespace] = JSON.parse(fs.readFileSync(path.join(englishLocaleRoot, entry), "utf8"))
  }

  return bundles
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const localeKeys = collectLocaleKeys(loadEnglishBundles())
  const references = mergeReferences(
    walkFiles(srcRoot).map((file) => collectKeyReferencesFromSource(
      fs.readFileSync(file, "utf8"),
      path.relative(webRoot, file)
    ))
  )

  const unusedKeys = findUnusedKeys(localeKeys, references)

  if (unusedKeys.length > 0) {
    console.error(`Locale keys with no reachable reference in src (${unusedKeys.length}):\n`)
    for (const key of unusedKeys) {
      console.error(`- ${key}`)
    }
    console.error("\nDelete them from every locale. If one is used, teach this scanner to see the reference.")
    process.exit(1)
  }

  console.log(`${localeKeys.length} English locale keys checked, all reachable.`)
}
