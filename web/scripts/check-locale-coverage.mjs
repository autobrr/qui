import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const localesRoot = path.join(webRoot, "src", "i18n", "locales")
const enRoot = path.join(localesRoot, "en")

// Plural rules are written out per locale, never derived from CLDR: i18next resolves a category a
// locale lacks against English instead of against another category, and the categories a locale
// actually needs are narrower than CLDR lists. ko has no `one` at all; cs `many` covers decimals
// only, which every count is floored past; fr/it/ca/pt-BR `many` fires at exact millions alone.
// `hasOne: false` lets a locale omit `_one`; a required category is satisfied by an unsuffixed
// catch-all key, which i18next answers every category with.
const localeRules = {
  fr: {},
  de: {},
  it: {},
  ko: { hasOne: false },
  "pt-BR": {},
  ca: {},
  cs: { pluralForms: { few: "required", many: "optional" } },
  uk: { pluralForms: { few: "required", many: "required" } },
  "zh-CN": { hasOne: false, cjk: true },
  "zh-TW": { hasOne: false, cjk: true },
}

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

// {{plural}} carries an English "s"/"ies" suffix, so a translation has no use for it. PR #2784
// converts those strings to real plural keys; this set goes with the last call site.
const englishOnlyVars = new Set(["plural"])

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

function containsCJK(str) {
  return /[一-鿿㐀-䶿]/.test(str)
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

// Bases English pluralizes. Only these require plural forms from a locale, and only these make a
// locale-only `_few`/`_many` key legitimate rather than a leftover.
function englishPluralBases(enFlat) {
  const bases = new Set()

  for (const key of enFlat.keys()) {
    if (!key.endsWith("_one")) continue
    const base = key.slice(0, -"_one".length)
    if (enFlat.has(`${base}_other`)) {
      bases.add(base)
    }
  }

  return bases
}

// The English key a locale key is compared against: itself, or the `_other` form behind a category
// English does not have, so a cs `_few` value is still checked for placeholders and markup.
function comparableEnKey(key, enFlat, rule) {
  if (enFlat.has(key)) return key

  for (const category of Object.keys(rule.pluralForms ?? {})) {
    const suffix = `_${category}`
    if (!key.endsWith(suffix)) continue
    const base = key.slice(0, -suffix.length)
    if (enFlat.has(`${base}_other`)) return `${base}_other`
  }

  return null
}

// ---------------------------------------------------------------------------
// Check functions
// ---------------------------------------------------------------------------

function checkMissingKeys(enFlat, localeFlat, namespace, rule, pluralBases) {
  const errors = []

  for (const [key, value] of enFlat) {
    if (rule.hasOne === false && key.endsWith("_one") && pluralBases.has(key.slice(0, -"_one".length))) {
      continue
    }

    if (!localeFlat.has(key)) {
      const truncated = value.length > 80 ? `${value.slice(0, 77)}...` : value
      errors.push(`${namespace}.${key}: ${JSON.stringify(truncated)}`)
    }
  }

  return errors
}

function checkExtraKeys(enFlat, localeFlat, namespace, rule, pluralBases) {
  const errors = []

  for (const key of localeFlat.keys()) {
    if (enFlat.has(key)) continue

    const enKey = comparableEnKey(key, enFlat, rule)
    if (enKey && pluralBases.has(enKey.slice(0, -"_other".length))) continue

    errors.push(`${namespace}.${key}`)
  }

  return errors
}

// Without its required categories a locale renders English at those counts, which reads as a working
// translation. checkMissingKeys already covers the categories English itself has.
function checkPluralForms(localeFlat, namespace, rule, pluralBases) {
  const errors = []

  for (const [category, requirement] of Object.entries(rule.pluralForms ?? {})) {
    if (requirement !== "required") continue

    for (const base of pluralBases) {
      if (localeFlat.has(base) || localeFlat.has(`${base}_${category}`)) continue
      errors.push(`${namespace}.${base}_${category}`)
    }
  }

  return errors
}

function checkInterpolation(enValue, localeValue, key, namespace, locale) {
  const errors = []
  const enVars = extractInterpolationVars(enValue)
  const localeVars = extractInterpolationVars(localeValue)

  for (const v of enVars) {
    if (englishOnlyVars.has(v)) continue
    if (!localeVars.has(v)) {
      errors.push(`${namespace}.${key}: missing {{${v}}} in ${locale}`)
    }
  }

  for (const v of localeVars) {
    if (enVars.has(v)) continue
    errors.push(`${namespace}.${key}: extra {{${v}}} not in en`)
  }

  return errors
}

function checkHtmlTags(enValue, localeValue, key, namespace) {
  const enTags = extractHtmlTags(enValue).sort()
  const localeTags = extractHtmlTags(localeValue).sort()

  if (enTags.join(",") === localeTags.join(",")) return []

  const missing = enTags.filter((t) => !localeTags.includes(t))
  const extra = localeTags.filter((t) => !enTags.includes(t))
  const parts = []
  if (missing.length) parts.push(`missing <${missing.join(">, <")}>`)
  if (extra.length) parts.push(`extra <${extra.join(">, <")}>`)

  return [`${namespace}.${key}: ${parts.join(", ")}`]
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

function checkUnneededOneForms(localeFlat, namespace, locale) {
  const warnings = []

  for (const key of localeFlat.keys()) {
    if (key.endsWith("_one")) {
      warnings.push(`${namespace}.${key}: ${locale} does not need _one variant (only _other)`)
    }
  }

  return warnings
}

function checkTextLength(enFlat, localeFlat, namespace, locale) {
  const warnings = []

  for (const [key, enValue] of enFlat) {
    const localeValue = localeFlat.get(key)
    if (!localeValue || localeValue === enValue) continue

    const enClean = stripHtmlTags(stripInterpolation(enValue)).trim()
    const localeClean = stripHtmlTags(stripInterpolation(localeValue)).trim()

    if (enClean.length < 8) continue

    if (localeClean.length > enClean.length * 1.5) {
      const ratio = Math.round((localeClean.length / enClean.length) * 100)
      warnings.push(`${namespace}.${key}: ${locale} is ${ratio}% of en length (${localeClean.length} vs ${enClean.length} chars)`)
    }
  }

  return warnings
}

function checkPunctuation(localeFlat, namespace) {
  const warnings = []

  const halfToFull = {
    ",": "，",
    ";": "；",
    "!": "！",
    "?": "？",
    ":": "：",
  }

  for (const [key, value] of localeFlat) {
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

// English is the same for every locale, so a run over all of them parses each namespace once.
const englishCache = new Map()

function loadEnglish(ns) {
  let english = englishCache.get(ns)
  if (english) return english

  const enPath = path.join(enRoot, `${ns}.json`)
  if (!fs.existsSync(enPath)) {
    english = { missing: true }
  } else {
    try {
      const flat = flattenKeys(JSON.parse(fs.readFileSync(enPath, "utf8")))
      english = { flat, pluralBases: englishPluralBases(flat) }
    } catch {
      english = { unparsable: true }
    }
  }

  englishCache.set(ns, english)
  return english
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

function printSection(label, items, severity) {
  if (items.length === 0) return
  console.log(`[${label}] ${items.length} ${severity}${items.length === 1 ? "" : "s"}`)
  for (const item of items.sort()) {
    console.log(`  - ${item}`)
  }
  console.log()
}

function report(locale, errors, warnings) {
  const totalErrors = Object.values(errors).reduce((sum, arr) => sum + arr.length, 0)
  const totalWarnings = Object.values(warnings).reduce((sum, arr) => sum + arr.length, 0)

  if (totalErrors === 0 && totalWarnings === 0) {
    console.log(`${locale} translation coverage: all checks passed.`)
    return totalErrors
  }

  console.log(`=== ${locale} Translation Coverage Report ===\n`)

  if (totalErrors > 0) {
    console.log("ERRORS:\n")
    printSection("Missing Keys", errors.missingKeys, "error")
    printSection("Extra Keys", errors.extraKeys, "error")
    printSection("Plural Forms", errors.pluralForms, "error")
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

  return totalErrors
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function checkLocale(locale) {
  const rule = localeRules[locale]
  const localeRoot = path.join(localesRoot, locale)

  // A configured locale without a directory is a misconfiguration, not a locale still being added.
  if (!fs.existsSync(localeRoot)) {
    console.error(`${locale} locale directory not found: ${localeRoot}`)
    return 1
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

  const warnings = {
    untranslatedUnexplained: [],
    untranslatedExplained: [],
    pluralForms: [],
    textLength: [],
    punctuation: [],
  }

  for (const ns of namespaces) {
    const localePath = path.join(localeRoot, `${ns}.json`)
    const english = loadEnglish(ns)

    if (english.missing) {
      errors.missingKeys.push(`${ns}: English locale file missing`)
      continue
    }

    if (!fs.existsSync(localePath)) {
      errors.missingKeys.push(`${ns}: ${locale} locale file missing`)
      continue
    }

    errors.encoding.push(...checkEncoding(localePath))

    let localeData
    try {
      if (english.unparsable) throw new Error("english")
      localeData = JSON.parse(fs.readFileSync(localePath, "utf8"))
    } catch {
      errors.encoding.push(`${ns}: failed to parse JSON, skipping checks`)
      continue
    }

    const { flat: enFlat, pluralBases } = english
    const localeFlat = flattenKeys(localeData)

    errors.missingKeys.push(...checkMissingKeys(enFlat, localeFlat, ns, rule, pluralBases))
    errors.extraKeys.push(...checkExtraKeys(enFlat, localeFlat, ns, rule, pluralBases))
    errors.pluralForms.push(...checkPluralForms(localeFlat, ns, rule, pluralBases))

    for (const [key, localeValue] of localeFlat) {
      if (localeValue === "") {
        errors.emptyStrings.push(`${ns}.${key}`)
        continue
      }

      const enKey = comparableEnKey(key, enFlat, rule)
      if (!enKey) continue

      const enValue = enFlat.get(enKey)
      errors.interpolation.push(...checkInterpolation(enValue, localeValue, key, ns, locale))
      errors.htmlTags.push(...checkHtmlTags(enValue, localeValue, key, ns))
    }

    const untranslated = checkUntranslated(enFlat, localeFlat, ns)
    warnings.untranslatedUnexplained.push(...untranslated.unexplained)
    warnings.untranslatedExplained.push(...untranslated.explained)

    if (rule.hasOne === false) {
      warnings.pluralForms.push(...checkUnneededOneForms(localeFlat, ns, locale))
    }

    if (rule.cjk) {
      warnings.textLength.push(...checkTextLength(enFlat, localeFlat, ns, locale))
      warnings.punctuation.push(...checkPunctuation(localeFlat, ns))
    }
  }

  return report(locale, errors, warnings) > 0 ? 1 : 0
}

const requested = process.argv[2]
const supportedLocales = Object.keys(localeRules)

if (requested !== undefined && !supportedLocales.includes(requested)) {
  console.error(`Unknown locale "${requested}". Use one of: ${supportedLocales.join(", ")}`)
  process.exit(1)
}

const locales = requested === undefined ? supportedLocales : [requested]
let failed = 0
for (const locale of locales) {
  failed += checkLocale(locale)
}

process.exit(failed > 0 ? 1 : 0)
