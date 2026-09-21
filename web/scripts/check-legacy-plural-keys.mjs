import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const defaultLocalesRoot = path.join(webRoot, "src", "i18n", "locales")

function flattenKeys(obj, prefix = "") {
  const keys = []

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key

    if (typeof value === "object" && value !== null && !Array.isArray(value)) {
      keys.push(...flattenKeys(value, fullKey))
      continue
    }

    keys.push(fullKey)
  }

  return keys
}

// i18next v4 never looks up a `_plural` key, so the base renders its `_other` form
// (or the English fallback) at every count.
export function findLegacyPluralKeys(localesRoot = defaultLocalesRoot) {
  const found = []

  for (const locale of fs.readdirSync(localesRoot).sort()) {
    const localeRoot = path.join(localesRoot, locale)
    if (!fs.statSync(localeRoot).isDirectory()) continue

    for (const file of fs.readdirSync(localeRoot).sort()) {
      if (!file.endsWith(".json")) continue

      const namespace = file.slice(0, -".json".length)
      const data = JSON.parse(fs.readFileSync(path.join(localeRoot, file), "utf8"))

      for (const key of flattenKeys(data)) {
        if (key.endsWith("_plural")) {
          found.push(`${locale}/${namespace}.json: ${key}`)
        }
      }
    }
  }

  return found
}

if (process.argv[1] === import.meta.filename) {
  const found = findLegacyPluralKeys()

  if (found.length === 0) {
    console.log("No legacy _plural keys found.")
    process.exit(0)
  }

  console.error(`Legacy _plural keys (${found.length}):\n`)
  for (const entry of found) {
    console.error(`- ${entry}`)
  }
  console.error("\nThe pre-v4 _plural suffix no longer resolves. Use the CLDR suffixes")
  console.error("(_one/_two/_few/_many/_other) that the locale's plural rules require.")
  process.exit(1)
}
