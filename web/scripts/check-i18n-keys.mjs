import fs from "node:fs"
import path from "node:path"

const webRoot = path.resolve(import.meta.dirname, "..")
const srcRoot = path.join(webRoot, "src")

function parseNamespaces(source) {
  const namespaces = []

  for (const match of source.matchAll(/useTranslation\(\s*(?:"([^"]+)"|\[([^\]]+)\])\s*\)/g)) {
    const singleNamespace = match[1]
    const namespaceList = match[2]

    if (singleNamespace) {
      namespaces.push(singleNamespace)
      continue
    }

    if (!namespaceList) {
      continue
    }

    for (const namespaceMatch of namespaceList.matchAll(/"([^"]+)"/g)) {
      namespaces.push(namespaceMatch[1])
    }
  }

  return [...new Set(namespaces)]
}

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
  {
    file: "src/components/torrents/TorrentDialogs.tsx",
    literals: [
      "\"Failed to load selected torrent tags\"",
      "\"Update the display name for this torrent. This changes how it appears in qBittorrent and qui.\"",
      "\"Loading categories...\"",
      "\"Set all to Global\"",
      "\"Set upload limit (KB/s)\"",
      "\"Update Tracker\"",
      "\"Continue\"",
    ],
  },
  {
    file: "src/pages/CrossSeedPage.tsx",
    literals: [
      "\"No active instances. Add instances first.\"",
      "\"Cross-seed mode\"",
      "\"Regular\"",
      "\"Hardlink\"",
      "\"Reflink (copy-on-write)\"",
      "\"Target instances\"",
      "\"Target indexers\"",
      "\"Settings that apply to all cross-seed operations.\"",
      "\"Fallback to regular mode on error\"",
    ],
    patterns: [
      /fall back to regular mode using existing files\./,
    ],
  },
  {
    file: "src/pages/InstanceBackups.tsx",
    literals: [
      "\"Backup settings updated\"",
      "\"Settings applied to all instances\"",
      "\"Backup queued\"",
      "\"Select instance\"",
      "\"Loading instance capabilities...\"",
      "\"Backups unavailable for this instance\"",
      "\"Last backup\"",
      "\"Next scheduled backup\"",
      "\"Backup settings\"",
      "\"Restore backup\"",
      "\"Backup run deleted\"",
      "\"Failed to delete backup run\"",
      "\"Deleted all backups\"",
      "\"Failed to delete backups\"",
      "\"Failed to load restore plan\"",
      "\"Included all torrents\"",
      "\"Restore dry-run completed\"",
      "\"Restore executed\"",
      "\"Failed to execute restore\"",
    ],
    patterns: [
      /Excluded \$\{label\} from restore/,
      /Included \$\{label\}/,
    ],
  },
  {
    file: "src/pages/RSSPage.tsx",
    literals: [
      "\"Select instance\"",
      "\"Enable RSS\"",
      "\"Enable Auto-Download\"",
      "\"Feed name\"",
      "\"https://example.com/rss\"",
      "\"Download torrent\"",
      "\"Open link\"",
      "\"Mark as read\"",
      "\"Toggle details\"",
      "\"No filters\"",
      "\"Retry\"",
      "\"Failed to remove feed\"",
      "\"Failed to refresh feed\"",
      "\"Failed to mark all as read\"",
      "\"Failed to rename feed\"",
      "\"Failed to update feed URL\"",
      "\"Failed to mark as read\"",
      "\"Failed to update rule\"",
      "\"Failed to remove rule\"",
      "\"Failed to add feed\"",
      "\"Failed to create folder\"",
      "\"Failed to create rule\"",
    ],
  },
  {
    file: "src/pages/Torrents.tsx",
    literals: [
      "\">Filters<\"",
      "\"Torrent Details\"",
      "\"Torrent Creation Tasks\"",
    ],
  },
  {
    file: "src/components/dashboard-settings-dialog.tsx",
    literals: [
      "\"Layout Settings\"",
      "\"Dashboard Settings\"",
      "\"Sections\"",
      "\"Tracker Breakdown Defaults\"",
      "\"Default Sort\"",
      "\"Direction\"",
      "\"Descending\"",
      "\"Ascending\"",
      "\"Items Per Page\"",
    ],
  },
]

for (const file of files) {
  const source = fs.readFileSync(file, "utf8")
  const namespaces = parseNamespaces(source)
  const defaultNamespace = namespaces[0]

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

  for (const match of source.matchAll(/\bi18n\.t\("([^"]+)",\s*\{[\s\S]*?ns:\s*"([^"]+)"/g)) {
    const key = match[1]
    const namespace = match[2]

    if (!localeCache.has(namespace)) {
      localeCache.set(namespace, loadLocale(namespace))
    }

    const namespacedLocale = localeCache.get(namespace)
    if (!namespacedLocale || getNestedValue(namespacedLocale, key) === undefined) {
      missingKeys.push(`${path.relative(webRoot, file)}: ${namespace}.${key}`)
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

  for (const pattern of check.patterns ?? []) {
    if (pattern.test(source)) {
      hardcodedStringErrors.push(`${check.file}: contains hardcoded UI string matching ${pattern}`)
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
