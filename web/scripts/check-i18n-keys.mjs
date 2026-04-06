import fs from "node:fs"
import path from "node:path"
import { collectMissingKeysForSource } from "./check-i18n-keys-lib.mjs"

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

function loadLocale(namespace) {
  const localePath = path.join(srcRoot, "i18n", "locales", "en", `${namespace}.json`)
  if (!fs.existsSync(localePath)) {
    return null
  }

  return JSON.parse(fs.readFileSync(localePath, "utf8"))
}

const files = walk(srcRoot)
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
  {
    file: "src/components/settings/DateTimePreferencesForm.tsx",
    literals: [
      "\"Select your local timezone for accurate time display\"",
      "\"Select timezone\"",
      "\"Use 12-hour format (AM/PM)\"",
      "\"Choose how dates are displayed throughout the application\"",
      "\"Save Preferences\"",
    ],
  },
  {
    file: "src/components/settings/ExternalProgramsManager.tsx",
    literals: [
      "\"Create External Program\"",
      "\"Loading external programs...\"",
      "\"Failed to load external programs\"",
      "\"Program Path:\"",
      "\"Arguments Template:\"",
      "\"Add Path Mapping\"",
      "\"Launch in terminal window\"",
      "\"Enable this program\"",
    ],
  },
  {
    file: "src/components/cross-seed/DirScanTab.tsx",
    literals: [
      "\"No directories configured yet.\"",
      "\"Recent Scan Runs\"",
      "\"Reset scan progress?\"",
      "\"Match Mode\"",
      "\"Default Category\"",
      "\"Directory Path\"",
      "\"Target qBittorrent Instance\"",
      "\"Scan Interval (minutes)\"",
    ],
    patterns: [
      /Add a directory to start scanning\./,
      /Added on top of the global Dir Scan tags\./,
      /Any files not matched by a torrent will be flagged as orphans/,
    ],
  },
  {
    file: "src/components/instances/preferences/CompletionOverview.tsx",
    literals: [
      "\"Bypass Torznab cache\"",
      "\"Include filters\"",
      "\"Exclude filters\"",
      "\"Categories\"",
      "\"Tags\"",
      "\"Indexers\"",
    ],
    patterns: [
      /Torrents already tagged .*cross-seed.* are skipped\./,
      /Skip torrents in these categories\./,
      /Skip torrents with these tags\./,
    ],
  },
  {
    file: "src/components/instances/preferences/FileManagementForm.tsx",
    literals: [
      "\"Default Save Path\"",
      "\"Temporary Download Path\"",
      "\"Default Content Layout\"",
      "\"Create subfolder\"",
      "\"Don't create subfolder\"",
      "\"Run External Program\"",
      "\"Supported Placeholders (case sensitive)\"",
    ],
  },
  {
    file: "src/components/instances/preferences/OrphanScanSettingsForm.tsx",
    patterns: [
      /Files flagged as orphans will be deleted automatically without manual review\./,
      /Ensure your .*Ignore Paths.* are correctly configured or use hardlink\/reflink mode for dir scan\./,
    ],
  },
  {
    file: "src/components/instances/preferences/ReannounceOverview.tsx",
    patterns: [
      /Monitors .*stalled.* torrents and reannounces them when no tracker is healthy\./,
    ],
  },
  {
    file: "src/components/instances/preferences/WorkflowDialog.tsx",
    literals: [
      "\"Conditions (optional)\"",
      "\"Live impact preview\"",
      "\"Grouped condition groups\"",
      "\"Action\"",
      "\"Dry-run results\"",
      "\"Enable dry run?\"",
      "\"Add custom group\"",
      "\"Group ID\"",
      "\"Keys (select at least one)\"",
      "\"Ambiguous policy (advanced)\"",
    ],
    patterns: [
      /Delete requires at least one condition\./,
      /Invalid regex pattern/,
      /No current matches\./,
      /No torrents currently match this rule\./,
      /Confirming will save and enable this rule\./,
      /Confirming will save this rule\./,
    ],
  },
  {
    file: "src/components/instances/preferences/WorkflowsOverview.tsx",
    literals: [
      "\"Failed to load rules\"",
      "\"Clear Activity History\"",
      "\"Delete all\"",
      "\"Failed to load activity\"",
      "\"Loading activity...\"",
    ],
    patterns: [
      /Check connection to the instance\./,
      /Confirming will enable this rule immediately\./,
    ],
  },
  {
    file: "src/components/settings/ArrInstancesManager.tsx",
    literals: [
      "\"Loading ARR instances...\"",
      "\"Failed to load ARR instances\"",
      "\"Type *\"",
      "\"Name *\"",
      "\"Base URL *\"",
      "\"Basic Auth\"",
      "\"Basic Username\"",
      "\"Basic Password\"",
      "\"Priority\"",
      "\"Timeout (seconds)\"",
    ],
    patterns: [
      /No ARR instances configured\./,
      /Are you sure you want to delete/,
    ],
  },
  {
    file: "src/components/settings/ClientApiKeysManager.tsx",
    literals: [
      "\"Create Client API Key\"",
      "\"API Key Created\"",
      "\"Proxy URL\"",
      "\"Client Name\"",
      "\"qBittorrent Instance\"",
      "\"Delete Client API Key?\"",
    ],
    patterns: [
      /Created:/,
      /Last used:/,
      /Host:/,
    ],
  },
  {
    file: "src/components/settings/LogSettingsPanel.tsx",
    literals: [
      "\"Log Level\"",
      "\"Select log level\"",
      "\"Log File Path\"",
      "\"Leave empty for stdout only\"",
      "\"Max Size (MB)\"",
      "\"Max Backups\"",
      "\"Saving...\"",
      "\"Save Settings\"",
      "\"Search logs...\"",
      "\"Muted Messages\"",
    ],
  },
  {
    file: "src/components/settings/NotificationsManager.tsx",
    literals: [
      "\"Name\"",
      "\"Shoutrrr URL\"",
      "\"Enabled\"",
      "\"Events\"",
      "\"All events\"",
      "\"New Notification Target\"",
      "\"Edit Notification Target\"",
      "\"Delete notification target?\"",
    ],
    patterns: [
      /Use any Shoutrrr-supported URL scheme\./,
      /Toggle delivery for this target\./,
      /Loading event types…/,
      /Loading notification targets…/,
      /Failed to load notification targets/,
      /Update delivery settings for this target\./,
    ],
  },
  {
    file: "src/hooks/useTorrentActions.ts",
    literals: [
      "\"Torrent name cannot be empty\"",
      "\"Both original and new file paths are required\"",
      "\"File name unchanged\"",
      "\"Both original and new folder paths are required\"",
      "\"Folder name unchanged\"",
    ],
    patterns: [
      /Added tags to \$\{count\} \$\{torrentText\}/,
      /Removed tags from \$\{count\} \$\{torrentText\}/,
      /Enabled.*Auto TMM for \$\{count\} \$\{torrentText\}/,
      /Disabled.*sequential download for \$\{count\} \$\{torrentText\}/,
    ],
  },
  {
    file: "src/lib/protocol-handler.ts",
    literals: [
      "\"Open qui in a regular browser tab to register (the prompt may appear in the address bar, which PWAs don’t show).\"",
      "\"If prompted by your browser, please accept to complete registration.\"",
      "\"Chrome often shows this as a small protocol-handler icon in the address bar; if nothing appears, enable protocol handlers at chrome://settings/handlers.\"",
    ],
  },
  {
    file: "src/components/instances/preferences/NetworkDiscoveryForm.tsx",
    literals: [
      "\"Peer Discovery\"",
      "\"Enable DHT (decentralized network)\"",
      "\"Tracker Settings\"",
      "\"Protocol Encryption\"",
      "\"Resolve peer countries\"",
    ],
  },
  {
    file: "src/components/instances/preferences/AdvancedNetworkForm.tsx",
    literals: [
      "\"Showing all advanced options\"",
      "\"Tracker Settings\"",
      "\"Apply rate limit to μTP protocol\"",
      "\"Disk I/O & Memory\"",
      "\"Security & IP Filtering\"",
      "\"Coalesce reads & writes\"",
      "\"Async I/O Threads\"",
      "\"Peer Management\"",
      "\"Block peers on privileged ports\"",
    ],
  },
  {
    file: "src/components/instances/preferences/ConnectionSettingsForm.tsx",
    literals: [
      "\"Limited version details\"",
      "\"Listening Port\"",
      "\"Port for incoming connections\"",
      "\"Protocol Settings\"",
      "\"BitTorrent Protocol\"",
      "\"IP Filtering\"",
      "\"Network Interface\"",
      "\"Global maximum connections\"",
      "\"Outgoing Ports\"",
      "\"Apply IP filter to trackers\"",
    ],
  },
  {
    file: "src/components/instances/preferences/QueueManagementForm.tsx",
    literals: [
      "\"Enable Queueing\"",
      "\"Max Active Downloads\"",
      "\"Max Active Uploads\"",
      "\"Max Active Torrents\"",
      "\"Max Checking Torrents\"",
    ],
  },
  {
    file: "src/components/instances/preferences/SeedingLimitsForm.tsx",
    literals: [
      "\"Enable Share Ratio Limit\"",
      "\"Maximum Share Ratio\"",
      "\"Enable Seeding Time Limit\"",
      "\"Maximum Seeding Time (minutes)\"",
    ],
  },
  {
    file: "src/components/instances/preferences/InstanceSettingsPanel.tsx",
    literals: [
      "\"Skip TLS Verification\"",
      "\"Local Filesystem Access\"",
      "\"qBittorrent Login\"",
      "\"Leave empty to keep current\"",
      "\"HTTP Basic Authentication\"",
      "\"Username\"",
      "\"Password\"",
      "\"Password required\"",
    ],
  },
  {
    file: "src/components/instances/preferences/SpeedLimitsForm.tsx",
    literals: [
      "\"Download Limit\"",
      "\"Upload Limit\"",
      "\"Alternative Download Limit\"",
      "\"Alternative Upload Limit\"",
      "\"Schedule the use of alternative rate limits\"",
      "\"Every day\"",
      "\"From:\"",
      "\"When:\"",
      "\"0 (Unlimited)\"",
    ],
  },
]

for (const file of files) {
  const source = fs.readFileSync(file, "utf8")
  missingKeys.push(...collectMissingKeysForSource({
    source,
    relativePath: path.relative(webRoot, file),
    loadLocale,
  }))
}

for (const check of hardcodedStringChecks) {
  const filePath = path.join(webRoot, check.file)
  const source = fs.readFileSync(filePath, "utf8")

  for (const literal of check.literals ?? []) {
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
