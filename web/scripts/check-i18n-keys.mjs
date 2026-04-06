import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const srcRoot = path.join(webRoot, "src")

function walk(dir) {
  const entries = fs.readdirSync(dir, { withFileTypes: true })
  const files = []

  for (const entry of entries) {
    if (entry.name === "dist" || entry.name === "i18n") {
      continue
    }

    const fullPath = path.join(dir, entry.name)

    if (entry.isDirectory()) {
      files.push(...walk(fullPath))
      continue
    }

    if (/\.(ts|tsx|js|jsx)$/.test(entry.name)) {
      files.push(fullPath)
    }
  }

  return files
}

function getNestedValue(obj, key) {
  return key.split(".").reduce((current, part) => {
    if (current && Object.prototype.hasOwnProperty.call(current, part)) {
      return current[part]
    }

    return undefined
  }, obj)
}

function loadLocale(namespace) {
  const localePath = path.join(srcRoot, "i18n", "locales", "en", `${namespace}.json`)
  if (!fs.existsSync(localePath)) {
    return null
  }

  return JSON.parse(fs.readFileSync(localePath, "utf8"))
}

const files = walk(srcRoot)
const localeCache = new Map()
const missingKeys = []
const hardcodedStringErrors = []

const hardcodedStringChecks = [
  {
    file: "src/pages/Search.tsx",
    literals: [
      "Try: \"Sample Movie 2024\"",
      "\"IMDb ID\"",
      "\"Prowlarr\"",
      "\"No enabled indexers available. Please add and enable indexers in the\"",
    ],
  },
  {
    file: "src/components/instances/preferences/WorkflowPreviewDialog.tsx",
    literals: [
      "\"Seeders\"",
      "\"Hardlinks\"",
      "\"Unregistered\"",
    ],
  },
  {
    file: "src/lib/dateTimeUtils.ts",
    literals: [
      "\"Just now\"",
      "\"Today\"",
      "\"Yesterday\"",
      "\"N/A\"",
    ],
  },
]

for (const file of files) {
  const source = fs.readFileSync(file, "utf8")
  const namespaceMatches = [...source.matchAll(/useTranslation\("([^"]+)"\)/g)]
  const defaultNamespace = namespaceMatches[0]?.[1]

  if (!defaultNamespace) {
    continue
  }

  if (!localeCache.has(defaultNamespace)) {
    localeCache.set(defaultNamespace, loadLocale(defaultNamespace))
  }

  const locale = localeCache.get(defaultNamespace)
  if (!locale) {
    missingKeys.push(`${path.relative(webRoot, file)}: missing locale file for namespace "${defaultNamespace}"`)
    continue
  }

  for (const match of source.matchAll(/\bt\("([^"]+)"/g)) {
    const key = match[1]
    if (getNestedValue(locale, key) === undefined) {
      missingKeys.push(`${path.relative(webRoot, file)}: ${defaultNamespace}.${key}`)
    }
  }
}

for (const check of hardcodedStringChecks) {
  const filePath = path.join(webRoot, check.file)
  const source = fs.readFileSync(filePath, "utf8")

  for (const literal of check.literals) {
    if (source.includes(literal)) {
      hardcodedStringErrors.push(`${check.file}: contains hardcoded UI string ${literal}`)
    }
  }
}

if (missingKeys.length > 0) {
  console.error("Missing translation keys:\n")
  for (const key of missingKeys.sort()) {
    console.error(`- ${key}`)
  }
  process.exit(1)
}

if (hardcodedStringErrors.length > 0) {
  console.error("Hardcoded UI strings:\n")
  for (const error of hardcodedStringErrors.sort()) {
    console.error(`- ${error}`)
  }
  process.exit(1)
}

console.log("All translation keys resolved.")
