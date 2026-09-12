import fs from "node:fs"
import path from "node:path"
import ts from "typescript"

const webRoot = path.resolve(import.meta.dirname, "..")
const srcRoot = path.join(webRoot, "src")
const englishLocaleRoot = path.join(srcRoot, "i18n", "locales", "en")

// i18next v4 CLDR plural suffixes. A locale leaf carrying one of these is part of a
// plural group; code references the group by its base key, never by a suffixed leaf.
const pluralSuffixPattern = /_(?:ordinal_)?(?:zero|one|two|few|many|other)$/

// Only literals shaped like a key path are worth matching. Keeps CSS values, URLs and
// prose out of the reference set.
const keyPathPattern = /^[A-Za-z0-9_$-]+(?::[A-Za-z0-9_$-]+)?(?:\.[A-Za-z0-9_$-]+)*$/

function stripNamespace(value) {
  const separatorIndex = value.indexOf(":")
  return separatorIndex === -1 ? value : value.slice(separatorIndex + 1)
}

function collectLeafKeys(node, prefix, leaves) {
  for (const [segment, value] of Object.entries(node)) {
    const keyPath = prefix ? `${prefix}.${segment}` : segment

    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      collectLeafKeys(value, keyPath, leaves)
      continue
    }

    leaves.push(keyPath)
  }
}

export function collectLocaleKeys(namespaceBundles) {
  const keys = []

  for (const [namespace, bundle] of Object.entries(namespaceBundles)) {
    const leaves = []
    collectLeafKeys(bundle, "", leaves)

    const seen = new Set()
    for (const leaf of leaves) {
      const baseKey = leaf.replace(pluralSuffixPattern, "")
      if (seen.has(baseKey)) {
        continue
      }

      seen.add(baseKey)
      keys.push({ namespace, key: baseKey })
    }
  }

  return keys
}

/**
 * Pulls every key reference a source file can contribute, in three flavours:
 *
 * - `literals`: whole key paths written out. Matching whole paths (never substrings)
 *   is what keeps `detailsPanel.banPeer` dead while `detailsPanel.banPeerPermanent`
 *   is live, and what keeps the three separate `shareLimit.*` subtrees apart.
 * - `prefixes`: the static head of a template like `dateTime.units.${unit}`, which
 *   reaches every leaf below it. Without this the whole block reads as dead.
 * - `suffixes`: the static tail of a template whose prefix is a variable, as in
 *   `` `${s}.dryRun` ``. The subtree is unknowable, so the leaf name alone counts.
 *
 * Literals are collected from anywhere in the file, not just inside `t(...)`: keys
 * travel through `labelKey` fields and `<Trans i18nKey>` attributes as often as they
 * are passed directly.
 */
export function collectKeyReferencesFromSource(source, fileName) {
  const sourceFile = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true)
  const literals = new Set()
  const prefixes = new Set()
  const suffixes = new Set()

  function addLiteral(rawValue) {
    const value = stripNamespace(rawValue)
    if (keyPathPattern.test(value)) {
      literals.add(value)
    }
  }

  // `dateTime.units.` -> `dateTime.units`; `sort.` -> `sort`. A head ending mid-segment
  // (`preferences.orphan` in `` `preferences.orphan${x}` ``) keeps only whole segments.
  function addPrefix(headText) {
    const lastDotIndex = headText.lastIndexOf(".")
    if (lastDotIndex === -1) {
      return
    }

    const prefix = stripNamespace(headText.slice(0, lastDotIndex))
    if (prefix && keyPathPattern.test(prefix)) {
      prefixes.add(prefix)
    }
  }

  function addSuffix(spanText) {
    if (!spanText.startsWith(".")) {
      return
    }

    const suffix = spanText.slice(1)
    if (suffix && keyPathPattern.test(suffix)) {
      suffixes.add(suffix)
    }
  }

  function visit(node) {
    if (ts.isStringLiteralLike(node)) {
      addLiteral(node.text)
    } else if (ts.isTemplateExpression(node)) {
      if (node.head.text) {
        addPrefix(node.head.text)
      } else {
        // `${variable}.leafName` — only the trailing segment is knowable.
        addSuffix(node.templateSpans[0].literal.text)
      }
    }

    ts.forEachChild(node, visit)
  }

  visit(sourceFile)

  return { literals, prefixes, suffixes }
}

// Keys that were already dead when this checker landed. The checker is a ratchet: this
// list may shrink, never grow. Removing a key here means deleting it from all ten
// locales in the same change. Entries are `namespace:key.path`.
const knownUnusedKeys = new Set([
  "automations:queryBuilder.durationUnits.seconds",
  "common:actions.toggle",
  "crossseed:dirScan.directoryDialog.categoryLabel",
  "crossseed:dirScan.directoryDialog.categoryPlaceholder",
  "crossseed:dirScan.directoryDialog.matchModeLabel",
  "crossseed:dirScan.directoryDialog.matchModeLink",
  "crossseed:dirScan.directoryDialog.matchModeLinkDescription",
  "crossseed:dirScan.directoryDialog.matchModeSearch",
  "crossseed:dirScan.directoryDialog.matchModeSearchDescription",
  "crossseed:dirScan.directoryDialog.selectTags",
  "crossseed:dirScan.directoryDialog.tagsLabel",
  "crossseed:dirScan.directoryDialog.toleranceLabel",
  "crossseed:dirScan.noRunsRecorded",
  "crossseed:dirScan.resetFiles",
  "crossseed:dirScan.resetFilesConfirm",
  "crossseed:dirScan.resetFilesConfirmDescription",
  "crossseed:dirScan.resetFilesDescription",
  "crossseed:dirScan.runHistory",
  "crossseed:dirScan.settingsDialog.defaultInstance",
  "crossseed:dirScan.settingsDialog.defaultIntervalLabel",
  "crossseed:dirScan.settingsDialog.defaultToleranceLabel",
  "crossseed:dirScan.settingsDialog.selectDefaultInstance",
  "crossseed:dirScan.statusLabels.partial",
  "crossseed:dirScan.statusLabels.pending",
  "crossseed:dirScan.statusLabels.running",
  "instances:card.tooltips.instanceSettings",
  "instances:form.labels.authBypass",
  "instances:form.labels.authBypassDescription",
  "instances:preferences.orphanScanOverview.orphanFilesFound_plural",
  "instances:preferences.orphanScanOverview.pathsIgnored_plural",
  "instances:preferences.orphanScanPreview.filesCount_plural",
  "instances:preferences.reannounceOverview.confirmEnableImmediately",
  "instances:preferences.settingsPanel.labels.qbittorrentLogin",
  "instances:preferences.settingsPanel.labels.qbittorrentLoginDescription",
  "instances:preferences.speedLimits.schedule",
  "instances:preferences.workflows.actionCount",
  "instances:preferences.workflows.addOnly",
  "instances:preferences.workflows.addRule",
  "instances:preferences.workflows.applyNow",
  "instances:preferences.workflows.autoTmmOff",
  "instances:preferences.workflows.autoTmmOn",
  "instances:preferences.workflows.delete",
  "instances:preferences.workflows.deleteKeepFiles",
  "instances:preferences.workflows.deleteWithFiles",
  "instances:preferences.workflows.deleteWithFilesIncludeCrossSeeds",
  "instances:preferences.workflows.deleteWithFilesPreserveCrossSeeds",
  "instances:preferences.workflows.description",
  "instances:preferences.workflows.disabled",
  "instances:preferences.workflows.download",
  "instances:preferences.workflows.edit",
  "instances:preferences.workflows.externalProgram",
  "instances:preferences.workflows.fullSync",
  "instances:preferences.workflows.loadingRules",
  "instances:preferences.workflows.moveToCategory",
  "instances:preferences.workflows.noActionsSet",
  "instances:preferences.workflows.noAutomations",
  "instances:preferences.workflows.noTags",
  "instances:preferences.workflows.programId",
  "instances:preferences.workflows.ratio",
  "instances:preferences.workflows.removeOnly",
  "instances:preferences.workflows.seedTime",
  "instances:preferences.workflows.shareLimits",
  "instances:preferences.workflows.speedLimits",
  "instances:preferences.workflows.title",
  "instances:preferences.workflows.trackerDerivedTag",
  "instances:preferences.workflows.upload",
  "instances:preferences.workflowsOverview.executed",
  "instances:preferences.workflowsOverview.hashCopied",
  "instances:preferences.workflowsOverview.noInstancesTitle",
  "instances:preferences.workflowsOverview.removed",
  "instances:preferences.workflowsOverview.summary.deleteLabelCondition",
  "instances:preferences.workflowsOverview.summary.deleteLabelRatio",
  "instances:preferences.workflowsOverview.summary.deleteLabelSeeding",
  "instances:preferences.workflowsOverview.summary.deleteLabelUnregistered",
  "rss:feeds.addFeed",
  "rss:feeds.addFolder",
  "search:results.noInfoUrlAvailable",
  "search:results.noValue",
  "settings:indexers.activity.cooldown",
  "settings:indexers.activity.idle",
  "settings:indexers.activity.queueLength",
  "settings:indexers.activity.workers",
  "settings:indexers.addIndexer",
  "settings:indexers.autodiscoverProwlarr",
  "settings:indexers.autodiscovery.toast.importErrors",
  "settings:indexers.noIndexers",
  "settings:indexers.noIndexersDescription",
  "settings:indexers.toast.allDeletedFailed",
  "settings:indexers.toast.allDeletedSuccess",
  "settings:themes.custom.directoryLabel",
  "settings:themes.license.toasts.activationSuccess",
  "settings:themes.license.toasts.refreshFailed",
  "settings:themes.license.toasts.refreshedAll",
  "settings:themes.selector.freeBadge",
  "settings:themes.selector.freeThemes",
  "settings:themes.selector.premiumBadge",
  "settings:themes.selector.premiumThemes",
  "torrents:detailsPanel.toast.fileRenameFailed",
  "torrents:detailsPanel.toast.folderRenameFailed",
  "torrents:fileTree.toggle",
  "torrents:generalTab.connections",
  "torrents:generalTab.downSpeed",
  "torrents:generalTab.eta",
  "torrents:generalTab.info",
  "torrents:generalTab.lastActivity",
  "torrents:generalTab.lastSeen",
  "torrents:generalTab.upSpeed",
  "torrents:peersTable.relevance",
  "torrents:tableColumns.state",
  "torrents:trackersTable.tier",
])

function isReachable(key, references) {
  if (references.literals.has(key)) {
    return true
  }

  for (const prefix of references.prefixes) {
    if (key === prefix || key.startsWith(`${prefix}.`)) {
      return true
    }
  }

  for (const suffix of references.suffixes) {
    if (key === suffix || key.endsWith(`.${suffix}`)) {
      return true
    }
  }

  return false
}

export function findUnusedKeys(localeKeys, references) {
  return localeKeys
    .filter(({ key }) => !isReachable(key, references))
    .map(({ namespace, key }) => `${namespace}:${key}`)
    .sort()
}

export function mergeReferences(referenceSets) {
  const merged = { literals: new Set(), prefixes: new Set(), suffixes: new Set() }

  for (const references of referenceSets) {
    for (const literal of references.literals) merged.literals.add(literal)
    for (const prefix of references.prefixes) merged.prefixes.add(prefix)
    for (const suffix of references.suffixes) merged.suffixes.add(suffix)
  }

  return merged
}

function walkSourceFiles(dir) {
  const files = []

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "dist" || entry.name === "locales") {
      continue
    }

    const fullPath = path.join(dir, entry.name)

    if (entry.isDirectory()) {
      files.push(...walkSourceFiles(fullPath))
      continue
    }

    if (/\.(ts|tsx|js|jsx|mjs)$/.test(entry.name)) {
      files.push(fullPath)
    }
  }

  return files
}

function loadEnglishBundles() {
  const bundles = {}

  for (const entry of fs.readdirSync(englishLocaleRoot)) {
    if (!entry.endsWith(".json")) {
      continue
    }

    const namespace = entry.slice(0, -".json".length)
    bundles[namespace] = JSON.parse(fs.readFileSync(path.join(englishLocaleRoot, entry), "utf8"))
  }

  return bundles
}

if (process.argv[1] === import.meta.filename) {
  const localeKeys = collectLocaleKeys(loadEnglishBundles())
  const references = mergeReferences(
    walkSourceFiles(srcRoot).map((file) => collectKeyReferencesFromSource(
      fs.readFileSync(file, "utf8"),
      path.relative(webRoot, file)
    ))
  )

  const unusedKeys = findUnusedKeys(localeKeys, references)
  const newlyUnusedKeys = unusedKeys.filter((key) => !knownUnusedKeys.has(key))
  const drainedKeys = [...knownUnusedKeys].filter((key) => !unusedKeys.includes(key)).sort()

  if (drainedKeys.length > 0) {
    console.log(`${drainedKeys.length} allowlisted key(s) are no longer unused; drop them from knownUnusedKeys:\n`)
    for (const key of drainedKeys) {
      console.log(`- ${key}`)
    }
    console.log("")
  }

  if (newlyUnusedKeys.length > 0) {
    console.error(`Locale keys with no reachable reference in src (${newlyUnusedKeys.length}):\n`)
    for (const key of newlyUnusedKeys) {
      console.error(`- ${key}`)
    }
    console.error("\nDelete them from every locale, or reference them from the UI.")
    process.exit(1)
  }

  console.log(
    `${localeKeys.length} English locale keys checked, ` +
    `${unusedKeys.length} known-unused allowlisted, no new unused keys.`
  )
}
