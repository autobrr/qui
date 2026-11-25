import fs from "node:fs"
import path from "node:path"

const scriptPath = fs.realpathSync(import.meta.filename)
const webRoot = path.resolve(path.dirname(scriptPath), "..")
const defaultLocalesRoot = path.join(webRoot, "src", "i18n", "locales")

function stringsWithBackticks(obj, prefix = "") {
  const found = []

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key

    if (typeof value === "object" && value !== null) {
      found.push(...stringsWithBackticks(value, fullKey))
    } else if (typeof value === "string" && value.includes("`")) {
      found.push(fullKey)
    }
  }

  return found
}

export function findLocaleBackticks(localesRoot = defaultLocalesRoot) {
  const found = []

  for (const locale of fs.readdirSync(localesRoot).sort()) {
    const localeRoot = path.join(localesRoot, locale)
    if (!fs.statSync(localeRoot).isDirectory()) continue

    for (const file of fs.readdirSync(localeRoot).sort()) {
      if (!file.endsWith(".json")) continue

      const data = JSON.parse(fs.readFileSync(path.join(localeRoot, file), "utf8"))
      for (const key of stringsWithBackticks(data)) {
        found.push(`${locale}/${file}: ${key}`)
      }
    }
  }

  return found
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const found = findLocaleBackticks()

  if (found.length === 0) {
    console.log("No backticks in locale strings.")
    process.exit(0)
  }

  console.error(`Backticks in locale strings (${found.length}):\n`)
  for (const entry of found) {
    console.error(`- ${entry}`)
  }
  console.error("\nThe UI shows locale text as written, so a backtick appears on screen. Use plain words,")
  console.error("or wrap the code in <code></code> and render the key with <Trans> and a code component.")
  process.exit(1)
}
