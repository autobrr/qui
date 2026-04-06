import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const localesRoot = path.join(webRoot, "src", "i18n", "locales")
const enRoot = path.join(localesRoot, "en")
const zhCNRoot = path.join(localesRoot, "zh-CN")

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

// Technical terms, brand names, and abbreviations that are acceptable
// to leave untranslated in zh-CN.
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

// i18next v4 CLDR plural suffixes.
const pluralSuffixes = ["_zero", "_one", "_two", "_few", "_many", "_other"]

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

function getPluralSuffix(key) {
  for (const suffix of pluralSuffixes) {
    if (key.endsWith(suffix)) {
      return suffix
    }
  }

  if (key.endsWith("_plural")) {
    return "_plural"
  }

  return null
}

function containsCJK(str) {
  return /[\u4e00-\u9fff\u3400-\u4dbf]/.test(str)
}

function stripInterpolation(str) {
  return str.replace(/\{\{[^}]+\}\}/g, "")
}

function stripHtmlTags(str) {
  return str.replace(/<\/?[^>]+>/g, "")
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

function checkMissingKeys(enFlat, zhFlat, namespace) {
  const errors = []

  // Build set of en plural bases that have _one/_other pairs (v4 CLDR style).
  // For these, zh-CN only needs _other -- _one is not required.
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
    // Skip _one keys for v4 plural pairs -- zh-CN collapses to _other only.
    if (key.endsWith("_one") && v4PluralBases.has(key.slice(0, -4))) {
      continue
    }

    if (!zhFlat.has(key)) {
      const truncated = value.length > 80 ? `${value.slice(0, 77)}...` : value
      errors.push(`${namespace}.${key}: ${JSON.stringify(truncated)}`)
    }
  }

  return errors
}

function checkExtraKeys(enFlat, zhFlat, namespace) {
  const errors = []

  for (const key of zhFlat.keys()) {
    if (!enFlat.has(key)) {
      // If zh-CN has _other and en has _one/_other pair, that is fine.
      if (key.endsWith("_other") && enFlat.has(`${key.slice(0, -6)}_one`)) {
        continue
      }

      errors.push(`${namespace}.${key}`)
    }
  }

  return errors
}

// Interpolation variables that exist only for English grammar (e.g. appending "s")
// and can be safely omitted in languages that do not use suffix-based pluralization.
const localeSpecificVars = new Set(["plural"])

function checkInterpolation(enFlat, zhFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const zhValue = zhFlat.get(key)
    if (!zhValue) continue

    const enVars = extractInterpolationVars(enValue)
    const zhVars = extractInterpolationVars(zhValue)

    for (const v of enVars) {
      if (localeSpecificVars.has(v)) continue
      if (!zhVars.has(v)) {
        errors.push(`${namespace}.${key}: missing {{${v}}} in zh-CN`)
      }
    }
  }

  return errors
}

function checkHtmlTags(enFlat, zhFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const zhValue = zhFlat.get(key)
    if (!zhValue) continue

    const enTags = extractHtmlTags(enValue).sort()
    const zhTags = extractHtmlTags(zhValue).sort()

    if (enTags.join(",") !== zhTags.join(",")) {
      const missing = enTags.filter((t) => !zhTags.includes(t))
      const extra = zhTags.filter((t) => !enTags.includes(t))
      const parts = []
      if (missing.length) parts.push(`missing <${missing.join(">, <")}>`)
      if (extra.length) parts.push(`extra <${extra.join(">, <")}>`)
      errors.push(`${namespace}.${key}: ${parts.join(", ")}`)
    }
  }

  return errors
}

function checkEmptyStrings(zhFlat, namespace) {
  const errors = []

  for (const [key, value] of zhFlat) {
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

function checkUntranslated(enFlat, zhFlat, namespace) {
  const explained = []
  const unexplained = []

  for (const [key, enValue] of enFlat) {
    const zhValue = zhFlat.get(key)
    if (!zhValue) continue
    if (zhValue !== enValue) continue
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

function checkPluralForms(zhFlat, namespace) {
  const warnings = []

  for (const key of zhFlat.keys()) {
    if (key.endsWith("_one")) {
      warnings.push(`${namespace}.${key}: Chinese does not need _one variant (only _other)`)
    }
  }

  return warnings
}

function checkTextLength(enFlat, zhFlat, namespace) {
  const warnings = []

  for (const [key, enValue] of enFlat) {
    const zhValue = zhFlat.get(key)
    if (!zhValue || zhValue === enValue) continue

    const enClean = stripHtmlTags(stripInterpolation(enValue)).trim()
    const zhClean = stripHtmlTags(stripInterpolation(zhValue)).trim()

    if (enClean.length < 8) continue

    if (zhClean.length > enClean.length * 1.5) {
      const ratio = Math.round((zhClean.length / enClean.length) * 100)
      warnings.push(`${namespace}.${key}: zh-CN is ${ratio}% of en length (${zhClean.length} vs ${enClean.length} chars)`)
    }
  }

  return warnings
}

function checkPunctuation(zhFlat, namespace) {
  const warnings = []

  const halfToFull = {
    ",": "\uFF0C",
    ";": "\uFF1B",
    "!": "\uFF01",
    "?": "\uFF1F",
    ":": "\uFF1A",
  }

  for (const [key, value] of zhFlat) {
    if (!containsCJK(value)) continue

    // Strip interpolation and HTML before checking punctuation.
    const cleaned = stripHtmlTags(stripInterpolation(value))

    for (const [half, full] of Object.entries(halfToFull)) {
      if (cleaned.includes(half)) {
        warnings.push(`${namespace}.${key}: half-width "${half}" should be "${full}"`)
      }
    }
  }

  return warnings
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

if (!fs.existsSync(zhCNRoot)) {
  console.log("zh-CN locale directory not found, skipping coverage check.")
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
  pluralForms: [],
  textLength: [],
  punctuation: [],
}

for (const ns of namespaces) {
  const enPath = path.join(enRoot, `${ns}.json`)
  const zhPath = path.join(zhCNRoot, `${ns}.json`)

  if (!fs.existsSync(enPath)) {
    errors.missingKeys.push(`${ns}: English locale file missing`)
    continue
  }

  if (!fs.existsSync(zhPath)) {
    errors.missingKeys.push(`${ns}: zh-CN locale file missing`)
    continue
  }

  errors.encoding.push(...checkEncoding(zhPath))

  let enData, zhData
  try {
    enData = JSON.parse(fs.readFileSync(enPath, "utf8"))
    zhData = JSON.parse(fs.readFileSync(zhPath, "utf8"))
  } catch {
    errors.encoding.push(`${ns}: failed to parse JSON, skipping checks`)
    continue
  }

  const enFlat = flattenKeys(enData)
  const zhFlat = flattenKeys(zhData)

  errors.missingKeys.push(...checkMissingKeys(enFlat, zhFlat, ns))
  errors.extraKeys.push(...checkExtraKeys(enFlat, zhFlat, ns))
  errors.interpolation.push(...checkInterpolation(enFlat, zhFlat, ns))
  errors.htmlTags.push(...checkHtmlTags(enFlat, zhFlat, ns))
  errors.emptyStrings.push(...checkEmptyStrings(zhFlat, ns))

  const untranslated = checkUntranslated(enFlat, zhFlat, ns)
  warnings.untranslatedUnexplained.push(...untranslated.unexplained)
  warnings.untranslatedExplained.push(...untranslated.explained)
  warnings.pluralForms.push(...checkPluralForms(zhFlat, ns))
  warnings.textLength.push(...checkTextLength(enFlat, zhFlat, ns))
  warnings.punctuation.push(...checkPunctuation(zhFlat, ns))
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

const totalErrors = Object.values(errors).reduce((sum, arr) => sum + arr.length, 0)
const totalWarnings = Object.values(warnings).reduce((sum, arr) => sum + arr.length, 0)

if (totalErrors === 0 && totalWarnings === 0) {
  console.log("zh-CN translation coverage: all checks passed.")
  process.exit(0)
}

console.log("=== zh-CN Translation Coverage Report ===\n")

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
  printSection("Plural Forms", warnings.pluralForms, "warning")
  printSection("Text Length", warnings.textLength, "warning")
  printSection("Punctuation", warnings.punctuation, "warning")
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
