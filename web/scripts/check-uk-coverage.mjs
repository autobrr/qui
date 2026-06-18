import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const localesRoot = path.join(webRoot, "src", "i18n", "locales")
const enRoot = path.join(localesRoot, "en")
const ukRoot = path.join(localesRoot, "uk")

const namespaces = [
  "common",
  "auth",
  "settings",
  "torrents",
  "dashboard",
  "crossseed",
  "rss",
  "search",
  "instances",
  "automations",
]

const pluralSuffixes = ["_zero", "_one", "_two", "_few", "_many", "_other", "_plural"]
const requiredUkPluralSuffixes = ["_one", "_few", "_many", "_other"]
const localeSpecificVars = new Set(["plural"])

function flattenKeys(obj, prefix = "") {
  const result = new Map()

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key

    if (typeof value === "object" && value !== null && !Array.isArray(value)) {
      for (const [nestedKey, nestedValue] of flattenKeys(value, fullKey)) {
        result.set(nestedKey, nestedValue)
      }
    } else {
      result.set(fullKey, String(value))
    }
  }

  return result
}

function getPluralSuffix(key) {
  return pluralSuffixes.find((suffix) => key.endsWith(suffix)) ?? null
}

function extractInterpolationVars(str) {
  return new Set([...str.matchAll(/\{\{(\w+)\}\}/g)].map((match) => match[1]))
}

function extractHtmlTags(str) {
  return [...str.matchAll(/<\/?([a-zA-Z][a-zA-Z0-9]*)[^>]*>/g)]
    .map((match) => match[1].toLowerCase())
    .sort()
}

function hasBOM(buffer) {
  return buffer.length >= 3
    && buffer[0] === 0xEF
    && buffer[1] === 0xBB
    && buffer[2] === 0xBF
}

function checkEncoding(filePath) {
  const errors = []
  const buffer = fs.readFileSync(filePath)

  if (hasBOM(buffer)) {
    errors.push(`${path.basename(filePath)}: UTF-8 BOM detected, remove it`)
  }

  try {
    JSON.parse(buffer.toString("utf8"))
  } catch {
    errors.push(`${path.basename(filePath)}: invalid JSON or encoding`)
  }

  return errors
}

function englishPluralBases(enFlat) {
  const bases = new Set()
  for (const key of enFlat.keys()) {
    if (!key.endsWith("_one")) continue
    const base = key.slice(0, -4)
    if (enFlat.has(`${base}_other`)) {
      bases.add(base)
    }
  }
  return bases
}

function comparableEnKey(key, enFlat) {
  const suffix = getPluralSuffix(key)
  if (!suffix) return key

  const base = key.slice(0, -suffix.length)
  if (enFlat.has(`${base}_other`)) return `${base}_other`
  if (enFlat.has(`${base}_one`)) return `${base}_one`
  return key
}

function checkInterpolation(enKey, ukKey, enValue, ukValue, namespace) {
  const errors = []
  const enVars = extractInterpolationVars(enValue)
  const ukVars = extractInterpolationVars(ukValue)

  for (const v of enVars) {
    if (localeSpecificVars.has(v)) continue
    if (!ukVars.has(v)) {
      errors.push(`${namespace}.${ukKey}: missing {{${v}} from ${enKey}`)
    }
  }

  for (const v of ukVars) {
    if (localeSpecificVars.has(v)) continue
    if (!enVars.has(v)) {
      errors.push(`${namespace}.${ukKey}: extra {{${v}} not present in ${enKey}`)
    }
  }

  return errors
}

function checkHtmlTags(enKey, ukKey, enValue, ukValue, namespace) {
  const enTags = extractHtmlTags(enValue)
  const ukTags = extractHtmlTags(ukValue)

  if (enTags.join(",") === ukTags.join(",")) return []
  return [`${namespace}.${ukKey}: HTML tag mismatch compared to ${enKey}`]
}

if (!fs.existsSync(ukRoot)) {
  console.log("uk locale directory not found, skipping coverage check.")
  process.exit(0)
}

const errors = {
  missingKeys: [],
  extraKeys: [],
  pluralForms: [],
  interpolation: [],
  htmlTags: [],
  emptyStrings: [],
  encoding: [],
}

for (const ns of namespaces) {
  const enPath = path.join(enRoot, `${ns}.json`)
  const ukPath = path.join(ukRoot, `${ns}.json`)

  if (!fs.existsSync(enPath)) {
    errors.missingKeys.push(`${ns}: English locale file missing`)
    continue
  }

  if (!fs.existsSync(ukPath)) {
    errors.missingKeys.push(`${ns}: uk locale file missing`)
    continue
  }

  errors.encoding.push(...checkEncoding(ukPath))

  let enData, ukData
  try {
    enData = JSON.parse(fs.readFileSync(enPath, "utf8"))
    ukData = JSON.parse(fs.readFileSync(ukPath, "utf8"))
  } catch {
    errors.encoding.push(`${ns}: failed to parse JSON, skipping checks`)
    continue
  }

  const enFlat = flattenKeys(enData)
  const ukFlat = flattenKeys(ukData)
  const pluralBases = englishPluralBases(enFlat)

  for (const key of enFlat.keys()) {
    if (!ukFlat.has(key)) {
      errors.missingKeys.push(`${ns}.${key}`)
    }
  }

  for (const [key, value] of ukFlat) {
    if (value === "") {
      errors.emptyStrings.push(`${ns}.${key}`)
    }

    if (!enFlat.has(key)) {
      const suffix = getPluralSuffix(key)
      const base = suffix ? key.slice(0, -suffix.length) : null
      if (!(suffix && requiredUkPluralSuffixes.includes(suffix) && pluralBases.has(base))) {
        errors.extraKeys.push(`${ns}.${key}`)
        continue
      }
    }

    const enKey = comparableEnKey(key, enFlat)
    const enValue = enFlat.get(enKey)
    if (typeof enValue !== "string") continue

    errors.interpolation.push(...checkInterpolation(enKey, key, enValue, value, ns))
    errors.htmlTags.push(...checkHtmlTags(enKey, key, enValue, value, ns))
  }

  for (const base of pluralBases) {
    for (const suffix of requiredUkPluralSuffixes) {
      if (!ukFlat.has(`${base}${suffix}`)) {
        errors.pluralForms.push(`${ns}.${base}${suffix}`)
      }
    }
  }
}

const totalErrors = Object.values(errors).reduce((sum, arr) => sum + arr.length, 0)

if (totalErrors === 0) {
  console.log("uk translation coverage: all checks passed.")
  process.exit(0)
}

console.log("=== uk Translation Coverage Report ===\n")
for (const [label, items] of Object.entries(errors)) {
  if (items.length === 0) continue
  console.log(`[${label}] ${items.length} error${items.length === 1 ? "" : "s"}`)
  for (const item of items.sort()) {
    console.log(`  - ${item}`)
  }
  console.log()
}

process.exit(1)
