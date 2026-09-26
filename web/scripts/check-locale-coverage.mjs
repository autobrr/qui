import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const localesRoot = path.join(webRoot, "src", "i18n", "locales")
const enRoot = path.join(localesRoot, "en")
const supportedLocales = ["fr", "de", "it", "ko", "pt-BR", "ca"]
const locale = process.argv[2]
if (!supportedLocales.includes(locale)) {
  console.error(`Unknown locale "${locale}". Use one of: ${supportedLocales.join(", ")}`)
  process.exit(1)
}
const localeRoot = path.join(localesRoot, locale)

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

// These technical terms, brand names, and abbreviations can stay untranslated.
const passthroughTerms = new Set([
  "qBittorrent", "BitTorrent", "autobrr", "qui", "GitHub",
  "Prowlarr", "Jackett", "Sonarr", "Radarr", "Shoutrrr",
  "OpenID Connect", "OIDC", "OpenID",
  "DHT", "PEX", "UPnP", "NAT-PMP", "RSS", "SSE", "PWA",
  "TMM", "AutoTMM", "IPv4", "IPv6", "TCP", "UDP", "UTP",
  "SSL", "TLS", "HTTP", "HTTPS", "SOCKS5",
  "API", "JSON", "CSV", "URL", "IMDb", "TVDb",
  "Torznab", "Gazelle", "OPS", "RED",
  "FLAC", "MP3", "MKV", "REPACK", "PROPER",
  "Freeleech", "cross-seed",
  "KB/s", "MB/s", "KiB", "MiB", "GiB", "TiB",
  "KB", "MB", "GB", "TB", "B/s",
  "AM", "PM", "N/A", "I/O",
  "GPL-2.0-or-later",
])

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

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

function extractInterpolationVars(str) {
  const vars = new Set()
  for (const match of str.matchAll(/\{\{(\w+)\}\}/g)) {
    vars.add(match[1])
  }
  return vars
}

function extractHtmlTags(str) {
  const tags = []
  for (const match of str.matchAll(/<\/?([a-zA-Z][a-zA-Z0-9]*)[^>]*>/g)) {
    tags.push(match[1].toLowerCase())
  }
  return tags
}

function isPassthroughValue(value) {
  if (value.length <= 4) return true
  if (passthroughTerms.has(value)) return true
  if (/^[\d\s.,/:;()\-+%#*]+$/.test(value)) return true
  if (/^https?:\/\//.test(value)) return true

  // Check if the value is composed entirely of passthrough terms, whitespace,
  // and common punctuation / interpolation markers.
  const stripped = value
    .replace(/\{\{[^}]+\}\}/g, "")
    .replace(/[().,;:!?/\-\s]+/g, " ")
    .trim()

  if (!stripped) return true

  const words = stripped.split(/\s+/)
  return words.every((word) => passthroughTerms.has(word) || /^[\d]+$/.test(word))
}

function hasBOM(buffer) {
  return buffer.length >= 3
    && buffer[0] === 0xEF
    && buffer[1] === 0xBB
    && buffer[2] === 0xBF
}

// ---------------------------------------------------------------------------
// Check functions
// ---------------------------------------------------------------------------

function checkMissingKeys(enFlat, localeFlat, namespace) {
  const errors = []

  // Find English plural bases with both _one and _other forms.
  const v4PluralBases = new Set()
  for (const key of enFlat.keys()) {
    if (key.endsWith("_one")) {
      const base = key.slice(0, -4)
      if (enFlat.has(`${base}_other`)) {
        v4PluralBases.add(base)
      }
    }
  }

  for (const [key, value] of enFlat) {
    // Permit a locale to omit _one when English also has _other.
    if (key.endsWith("_one") && v4PluralBases.has(key.slice(0, -4))) {
      continue
    }

    if (!localeFlat.has(key)) {
      const truncated = value.length > 80 ? `${value.slice(0, 77)}...` : value
      errors.push(`${namespace}.${key}: ${JSON.stringify(truncated)}`)
    }
  }

  return errors
}

function checkExtraKeys(enFlat, localeFlat, namespace) {
  const errors = []

  for (const key of localeFlat.keys()) {
    if (!enFlat.has(key)) {
      // Permit an extra _other when English has _one.
      if (key.endsWith("_other") && enFlat.has(`${key.slice(0, -6)}_one`)) {
        continue
      }

      errors.push(`${namespace}.${key}`)
    }
  }

  return errors
}

// Permit translations to omit placeholders used only for English grammar.
const localeSpecificVars = new Set(["plural"])

function checkInterpolation(enFlat, localeFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const localeValue = localeFlat.get(key)
    if (!localeValue) continue

    const enVars = extractInterpolationVars(enValue)
    const localeVars = extractInterpolationVars(localeValue)

    for (const v of enVars) {
      if (localeSpecificVars.has(v)) continue
      if (!localeVars.has(v)) {
        errors.push(`${namespace}.${key}: missing {{${v}}} in ${locale}`)
      }
    }
  }

  return errors
}

function checkHtmlTags(enFlat, localeFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const localeValue = localeFlat.get(key)
    if (!localeValue) continue

    const enTags = extractHtmlTags(enValue).sort()
    const localeTags = extractHtmlTags(localeValue).sort()

    if (enTags.join(",") !== localeTags.join(",")) {
      const missing = enTags.filter((t) => !localeTags.includes(t))
      const extra = localeTags.filter((t) => !enTags.includes(t))
      const parts = []
      if (missing.length) parts.push(`missing <${missing.join(">, <")}>`)
      if (extra.length) parts.push(`extra <${extra.join(">, <")}>`)
      errors.push(`${namespace}.${key}: ${parts.join(", ")}`)
    }
  }

  return errors
}

function checkEmptyStrings(localeFlat, namespace) {
  const errors = []

  for (const [key, value] of localeFlat) {
    if (value === "") {
      errors.push(`${namespace}.${key}`)
    }
  }

  return errors
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

function classifyUntranslated(key, value) {
  if (/[/\\]/.test(value) && (/^[/\\]|^\w:[/\\]/.test(value) || /placeholder/i.test(key))) return "path"
  if (/^[\w+.-]+:\/\//.test(value)) return "url"
  if (/^[*?[\]{}|\\^$.,;:!@#%&()=<>_+\-\w\s]+$/.test(value) && /[*?|\\]/.test(value)) return "pattern"
  if (/placeholder/i.test(key) || /example/i.test(key)) return "example"
  if (passthroughTerms.has(value)) return "technical"

  // Check if composed of technical terms + connectors
  const stripped = value.replace(/[().,;:!?/\-\s]+/g, " ").trim()
  const words = stripped.split(/\s+/)
  if (words.length <= 4 && words.every((w) => passthroughTerms.has(w) || /^\d+$/.test(w) || w.length <= 2)) return "technical"

  return null
}

const untranslatedReasons = {
  path: "file path / placeholder",
  url: "URL / protocol",
  pattern: "glob / regex / filter pattern",
  example: "example value / placeholder",
  technical: "technical term",
}

function checkUntranslated(enFlat, localeFlat, namespace) {
  const explained = []
  const unexplained = []

  for (const [key, enValue] of enFlat) {
    const localeValue = localeFlat.get(key)
    if (!localeValue) continue
    if (localeValue !== enValue) continue
    if (isPassthroughValue(enValue)) continue

    const truncated = enValue.length > 60 ? `${enValue.slice(0, 57)}...` : enValue
    const reason = classifyUntranslated(key, enValue)

    if (reason) {
      explained.push(`${namespace}.${key}: ${JSON.stringify(truncated)} (${untranslatedReasons[reason]})`)
    } else {
      unexplained.push(`${namespace}.${key}: ${JSON.stringify(truncated)}`)
    }
  }

  return { explained, unexplained }
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

if (!fs.existsSync(localeRoot)) {
  console.log(`${locale} locale directory not found, skipping coverage check.`)
  process.exit(0)
}

const errors = {
  missingKeys: [],
  extraKeys: [],
  interpolation: [],
  htmlTags: [],
  emptyStrings: [],
  encoding: [],
}

const warnings = {
  untranslatedUnexplained: [],
  untranslatedExplained: [],
}

for (const ns of namespaces) {
  const enPath = path.join(enRoot, `${ns}.json`)
  const localePath = path.join(localeRoot, `${ns}.json`)

  if (!fs.existsSync(enPath)) {
    errors.missingKeys.push(`${ns}: English locale file missing`)
    continue
  }

  if (!fs.existsSync(localePath)) {
    errors.missingKeys.push(`${ns}: ${locale} locale file missing`)
    continue
  }

  errors.encoding.push(...checkEncoding(localePath))

  let enData, localeData
  try {
    enData = JSON.parse(fs.readFileSync(enPath, "utf8"))
    localeData = JSON.parse(fs.readFileSync(localePath, "utf8"))
  } catch {
    errors.encoding.push(`${ns}: failed to parse JSON, skipping checks`)
    continue
  }

  const enFlat = flattenKeys(enData)
  const localeFlat = flattenKeys(localeData)

  errors.missingKeys.push(...checkMissingKeys(enFlat, localeFlat, ns))
  errors.extraKeys.push(...checkExtraKeys(enFlat, localeFlat, ns))
  errors.interpolation.push(...checkInterpolation(enFlat, localeFlat, ns))
  errors.htmlTags.push(...checkHtmlTags(enFlat, localeFlat, ns))
  errors.emptyStrings.push(...checkEmptyStrings(localeFlat, ns))

  const untranslated = checkUntranslated(enFlat, localeFlat, ns)
  warnings.untranslatedUnexplained.push(...untranslated.unexplained)
  warnings.untranslatedExplained.push(...untranslated.explained)
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

const totalErrors = Object.values(errors).reduce((sum, arr) => sum + arr.length, 0)
const totalWarnings = Object.values(warnings).reduce((sum, arr) => sum + arr.length, 0)

if (totalErrors === 0 && totalWarnings === 0) {
  console.log(`${locale} translation coverage: all checks passed.`)
  process.exit(0)
}

console.log(`=== ${locale} Translation Coverage Report ===\n`)

function printSection(label, items, severity) {
  if (items.length === 0) return
  console.log(`[${label}] ${items.length} ${severity}${items.length === 1 ? "" : "s"}`)
  for (const item of items.sort()) {
    console.log(`  - ${item}`)
  }
  console.log()
}

if (totalErrors > 0) {
  console.log("ERRORS:\n")
  printSection("Missing Keys", errors.missingKeys, "error")
  printSection("Extra Keys", errors.extraKeys, "error")
  printSection("Interpolation", errors.interpolation, "error")
  printSection("HTML Tags", errors.htmlTags, "error")
  printSection("Empty Strings", errors.emptyStrings, "error")
  printSection("Encoding", errors.encoding, "error")
}

if (totalWarnings > 0) {
  console.log("WARNINGS:\n")
  printSection("Untranslated (needs review)", warnings.untranslatedUnexplained, "warning")
  printSection("Untranslated (kept intentionally - paths, URLs, patterns, examples, technical/community terms)", warnings.untranslatedExplained, "warning")
}

const errorParts = Object.entries(errors)
  .map(([key, arr]) => `${key}: ${arr.length}`)
  .join(", ")
const warnParts = Object.entries(warnings)
  .map(([key, arr]) => `${key}: ${arr.length}`)
  .join(", ")

console.log("=== Summary ===")
console.log(`  Errors:   ${totalErrors} (${errorParts})`)
console.log(`  Warnings: ${totalWarnings} (${warnParts})`)
console.log(`  Result:   ${totalErrors > 0 ? "FAIL" : "PASS (warnings only)"}`)

process.exit(totalErrors > 0 ? 1 : 0)
