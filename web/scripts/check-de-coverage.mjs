import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const localesRoot = path.join(webRoot, "src", "i18n", "locales")
const enRoot = path.join(localesRoot, "en")
const deRoot = path.join(localesRoot, "de")

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
// to leave untranslated in de.
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

function checkMissingKeys(enFlat, deFlat, namespace) {
  const errors = []

  // Build set of en plural bases that have _one/_other pairs (v4 CLDR style).
  // German uses the same one/other split as English, so _one IS expected --
  // but we tolerate locales that collapse to _other only (i18next falls back).
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
    // Skip _one keys for v4 plural pairs -- a locale providing only _other is valid.
    if (key.endsWith("_one") && v4PluralBases.has(key.slice(0, -4))) {
      continue
    }

    if (!deFlat.has(key)) {
      const truncated = value.length > 80 ? `${value.slice(0, 77)}...` : value
      errors.push(`${namespace}.${key}: ${JSON.stringify(truncated)}`)
    }
  }

  return errors
}

function checkExtraKeys(enFlat, deFlat, namespace) {
  const errors = []

  for (const key of deFlat.keys()) {
    if (!enFlat.has(key)) {
      // If de has _other and en has _one/_other pair, that is fine.
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

function checkInterpolation(enFlat, deFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const deValue = deFlat.get(key)
    if (!deValue) continue

    const enVars = extractInterpolationVars(enValue)
    const deVars = extractInterpolationVars(deValue)

    for (const v of enVars) {
      if (localeSpecificVars.has(v)) continue
      if (!deVars.has(v)) {
        errors.push(`${namespace}.${key}: missing {{${v}}} in de`)
      }
    }
  }

  return errors
}

function checkHtmlTags(enFlat, deFlat, namespace) {
  const errors = []

  for (const [key, enValue] of enFlat) {
    const deValue = deFlat.get(key)
    if (!deValue) continue

    const enTags = extractHtmlTags(enValue).sort()
    const deTags = extractHtmlTags(deValue).sort()

    if (enTags.join(",") !== deTags.join(",")) {
      const missing = enTags.filter((t) => !deTags.includes(t))
      const extra = deTags.filter((t) => !enTags.includes(t))
      const parts = []
      if (missing.length) parts.push(`missing <${missing.join(">, <")}>`)
      if (extra.length) parts.push(`extra <${extra.join(">, <")}>`)
      errors.push(`${namespace}.${key}: ${parts.join(", ")}`)
    }
  }

  return errors
}

function checkEmptyStrings(deFlat, namespace) {
  const errors = []

  for (const [key, value] of deFlat) {
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

function checkUntranslated(enFlat, deFlat, namespace) {
  const explained = []
  const unexplained = []

  for (const [key, enValue] of enFlat) {
    const deValue = deFlat.get(key)
    if (!deValue) continue
    if (deValue !== enValue) continue
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

if (!fs.existsSync(deRoot)) {
  console.log("de locale directory not found, skipping coverage check.")
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
  const dePath = path.join(deRoot, `${ns}.json`)

  if (!fs.existsSync(enPath)) {
    errors.missingKeys.push(`${ns}: English locale file missing`)
    continue
  }

  if (!fs.existsSync(dePath)) {
    errors.missingKeys.push(`${ns}: de locale file missing`)
    continue
  }

  errors.encoding.push(...checkEncoding(dePath))

  let enData, deData
  try {
    enData = JSON.parse(fs.readFileSync(enPath, "utf8"))
    deData = JSON.parse(fs.readFileSync(dePath, "utf8"))
  } catch {
    errors.encoding.push(`${ns}: failed to parse JSON, skipping checks`)
    continue
  }

  const enFlat = flattenKeys(enData)
  const deFlat = flattenKeys(deData)

  errors.missingKeys.push(...checkMissingKeys(enFlat, deFlat, ns))
  errors.extraKeys.push(...checkExtraKeys(enFlat, deFlat, ns))
  errors.interpolation.push(...checkInterpolation(enFlat, deFlat, ns))
  errors.htmlTags.push(...checkHtmlTags(enFlat, deFlat, ns))
  errors.emptyStrings.push(...checkEmptyStrings(deFlat, ns))

  const untranslated = checkUntranslated(enFlat, deFlat, ns)
  warnings.untranslatedUnexplained.push(...untranslated.unexplained)
  warnings.untranslatedExplained.push(...untranslated.explained)
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

const totalErrors = Object.values(errors).reduce((sum, arr) => sum + arr.length, 0)
const totalWarnings = Object.values(warnings).reduce((sum, arr) => sum + arr.length, 0)

if (totalErrors === 0 && totalWarnings === 0) {
  console.log("de translation coverage: all checks passed.")
  process.exit(0)
}

console.log("=== de Translation Coverage Report ===\n")

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
