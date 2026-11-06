import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const scriptPath = fileURLToPath(import.meta.url)
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

// i18next v4 never looks up a `_plural` key. The keys that shipped sat beside an
// unsuffixed base and no `_other`, so every count rendered the singular.
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

/** ESM main check; realpath avoids `/var` vs `/private/var` mismatch on macOS. */
let runAsMain = false
try {
	runAsMain =
		process.argv[1] != null &&
		fs.realpathSync.native(scriptPath) ===
			fs.realpathSync.native(process.argv[1])
} catch {
	runAsMain = false
}

if (runAsMain) {
  const found = findLegacyPluralKeys()

  if (found.length === 0) {
    console.log("No legacy _plural keys found.")
    process.exit(0)
  }

  console.error(`Legacy _plural keys (${found.length}):\n`)
  for (const entry of found) {
    console.error(`- ${entry}`)
  }
  console.error("\ni18next resolves no _plural key. Use the CLDR suffixes")
  console.error("(_one/_two/_few/_many/_other) that the locale's plural rules require.")
  process.exit(1)
}
