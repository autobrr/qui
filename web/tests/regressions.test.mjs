import assert from "node:assert/strict"
import { readFileSync, readdirSync } from "node:fs"
import path from "node:path"
import test from "node:test"

const webDir = process.cwd()
const localesDir = path.join(webDir, "src", "locales")

const requiredCommonKeys = [
  "completionOverview.filters.onlySelectedCategoriesMatched",
  "completionOverview.filters.onlySelectedIndexersQueried",
  "completionOverview.filters.onlySelectedTagsMatched",
  "crossSeedPage.auto.helper.instancesSelected",
  "crossSeedPage.auto.helper.onlySelectedTagsMatched",
  "crossSeedPage.seededSearch.help.onlySelectedIndexerQueried",
  "crossSeedPage.seededSearch.help.onlySelectedTorznabQueriedWithGazelle",
  "crossSeedPage.webhook.helper.onlySelectedTagsMatched",
  "globalStatusBar.selection.loadedCount",
  "trackersTable.columns.downloads",
  "trackersTable.columns.leeches",
  "trackersTable.columns.message",
  "trackersTable.columns.peers",
  "trackersTable.columns.seeds",
  "trackersTable.columns.status",
  "trackersTable.columns.tracker",
  "trackersTable.empty.noTrackers",
  "torrentDetailsPanel.count.files",
  "torrentDetailsPanel.count.sources",
  "torrentDetailsPanel.count.trackers",
  "torrentManagementBar.toolbarAria",
  "workflowDialog.fields.notify",
]

const requiredQueryBuilderTranslationKeys = [
  "fieldCombobox.fieldTypes.boolean",
  "fieldCombobox.fieldTypes.bytes",
  "fieldCombobox.fieldTypes.duration",
  "fieldCombobox.fieldTypes.float",
  "fieldCombobox.fieldTypes.hardlinkScope",
  "fieldCombobox.fieldTypes.integer",
  "fieldCombobox.fieldTypes.percentage",
  "fieldCombobox.fieldTypes.speed",
  "fieldCombobox.fieldTypes.state",
  "fieldCombobox.fieldTypes.string",
  "fieldCombobox.fields.addedOn",
  "fieldCombobox.fields.addedOnAge",
  "fieldCombobox.fields.amountLeft",
  "fieldCombobox.fields.autoManaged",
  "fieldCombobox.fields.availability",
  "fieldCombobox.fields.category",
  "fieldCombobox.fields.comment",
  "fieldCombobox.fields.completed",
  "fieldCombobox.fields.completionOn",
  "fieldCombobox.fields.completionOnAge",
  "fieldCombobox.fields.contentPath",
  "fieldCombobox.fields.contentType",
  "fieldCombobox.fields.createdBy",
  "fieldCombobox.fields.dlLimit",
  "fieldCombobox.fields.dlSpeed",
  "fieldCombobox.fields.downloadPath",
  "fieldCombobox.fields.downloaded",
  "fieldCombobox.fields.downloadedSession",
  "fieldCombobox.fields.effectiveName",
  "fieldCombobox.fields.eta",
  "fieldCombobox.fields.existsOnOtherInstance",
  "fieldCombobox.fields.existsOnSameInstance",
  "fieldCombobox.fields.firstLastPiecePrio",
  "fieldCombobox.fields.forceStart",
  "fieldCombobox.fields.freeSpace",
  "fieldCombobox.fields.groupSize",
  "fieldCombobox.fields.hardlinkScope",
  "fieldCombobox.fields.hash",
  "fieldCombobox.fields.hasMissingFiles",
  "fieldCombobox.fields.infohashV1",
  "fieldCombobox.fields.infohashV2",
  "fieldCombobox.fields.inactiveSeedingTimeLimit",
  "fieldCombobox.fields.isGrouped",
  "fieldCombobox.fields.isUnregistered",
  "fieldCombobox.fields.lastActivity",
  "fieldCombobox.fields.lastActivityAge",
  "fieldCombobox.fields.magnetUri",
  "fieldCombobox.fields.maxInactiveSeedingTime",
  "fieldCombobox.fields.maxRatio",
  "fieldCombobox.fields.maxSeedingTime",
  "fieldCombobox.fields.name",
  "fieldCombobox.fields.numComplete",
  "fieldCombobox.fields.numIncomplete",
  "fieldCombobox.fields.numLeechs",
  "fieldCombobox.fields.numSeeds",
  "fieldCombobox.fields.popularity",
  "fieldCombobox.fields.priority",
  "fieldCombobox.fields.private",
  "fieldCombobox.fields.progress",
  "fieldCombobox.fields.ratio",
  "fieldCombobox.fields.ratioLimit",
  "fieldCombobox.fields.reannounce",
  "fieldCombobox.fields.rlsAudio",
  "fieldCombobox.fields.rlsChannels",
  "fieldCombobox.fields.rlsCodec",
  "fieldCombobox.fields.rlsGroup",
  "fieldCombobox.fields.rlsHdr",
  "fieldCombobox.fields.rlsResolution",
  "fieldCombobox.fields.rlsSource",
  "fieldCombobox.fields.savePath",
  "fieldCombobox.fields.seedingOnOtherInstance",
  "fieldCombobox.fields.seedingOnSameInstance",
  "fieldCombobox.fields.seedingTime",
  "fieldCombobox.fields.seedingTimeLimit",
  "fieldCombobox.fields.seenComplete",
  "fieldCombobox.fields.sequentialDownload",
  "fieldCombobox.fields.size",
  "fieldCombobox.fields.state",
  "fieldCombobox.fields.superSeeding",
  "fieldCombobox.fields.systemDay",
  "fieldCombobox.fields.systemDayOfWeek",
  "fieldCombobox.fields.systemHour",
  "fieldCombobox.fields.systemMinute",
  "fieldCombobox.fields.systemMonth",
  "fieldCombobox.fields.systemYear",
  "fieldCombobox.fields.tags",
  "fieldCombobox.fields.timeActive",
  "fieldCombobox.fields.totalSize",
  "fieldCombobox.fields.tracker",
  "fieldCombobox.fields.trackers",
  "fieldCombobox.fields.trackersCount",
  "fieldCombobox.fields.upLimit",
  "fieldCombobox.fields.upSpeed",
  "fieldCombobox.fields.uploaded",
  "fieldCombobox.fields.uploadedSession",
  "fieldCombobox.groups.crossSeed",
  "fieldCombobox.groups.files",
  "fieldCombobox.groups.grouping",
  "fieldCombobox.groups.identity",
  "fieldCombobox.groups.mode",
  "fieldCombobox.groups.paths",
  "fieldCombobox.groups.peers",
  "fieldCombobox.groups.progress",
  "fieldCombobox.groups.release",
  "fieldCombobox.groups.size",
  "fieldCombobox.groups.speed",
  "fieldCombobox.groups.systemTime",
  "fieldCombobox.groups.time",
  "fieldCombobox.groups.tracker",
  "leafCondition.operator.labels.between",
  "leafCondition.operator.labels.contains",
  "leafCondition.operator.labels.containsIn",
  "leafCondition.operator.labels.endsWith",
  "leafCondition.operator.labels.eqSymbol",
  "leafCondition.operator.labels.equal",
  "leafCondition.operator.labels.existsIn",
  "leafCondition.operator.labels.gtSymbol",
  "leafCondition.operator.labels.gteSymbol",
  "leafCondition.operator.labels.is",
  "leafCondition.operator.labels.isNot",
  "leafCondition.operator.labels.ltSymbol",
  "leafCondition.operator.labels.lteSymbol",
  "leafCondition.operator.labels.matches",
  "leafCondition.operator.labels.notContains",
  "leafCondition.operator.labels.notEqSymbol",
  "leafCondition.operator.labels.notEqual",
  "leafCondition.operator.labels.startsWith",
]

function getNestedValue(object, dottedKey) {
  return dottedKey.split(".").reduce((current, part) => current?.[part], object)
}

function hasTranslation(common, key) {
  return getNestedValue(common, key) !== undefined
    || getNestedValue(common, `${key}_one`) !== undefined
    || getNestedValue(common, `${key}_other`) !== undefined
}

test("required common translation keys exist in every locale", () => {
  const localeDirs = readdirSync(localesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)

  for (const locale of localeDirs) {
    const common = JSON.parse(readFileSync(path.join(localesDir, locale, "common.json"), "utf8"))

    for (const key of requiredCommonKeys) {
      assert.ok(
        hasTranslation(common, key),
        `missing ${key} in ${locale}/common.json`,
      )
    }
  }
})

test("query builder label translations exist in every locale", () => {
  const localeDirs = readdirSync(localesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)

  for (const locale of localeDirs) {
    const common = JSON.parse(readFileSync(path.join(localesDir, locale, "common.json"), "utf8"))

    for (const key of requiredQueryBuilderTranslationKeys) {
      assert.ok(
        hasTranslation(common, key),
        `missing ${key} in ${locale}/common.json`,
      )
    }
  }
})

test("application info locale bundles are not copied verbatim from english", () => {
  const englishCommon = JSON.parse(readFileSync(path.join(localesDir, "en", "common.json"), "utf8"))
  const englishApplicationInfo = englishCommon.settingsPage?.applicationInfo

  assert.ok(englishApplicationInfo, "expected english applicationInfo translations to exist")

  const localeDirs = readdirSync(localesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== "en")
    .map((entry) => entry.name)

  for (const locale of localeDirs) {
    const common = JSON.parse(readFileSync(path.join(localesDir, locale, "common.json"), "utf8"))

    assert.notDeepEqual(
      common.settingsPage?.applicationInfo,
      englishApplicationInfo,
      `expected ${locale}/common.json applicationInfo translations to differ from english`,
    )
  }
})

test("service worker precaches the built app entry bundle", () => {
  const indexHtml = readFileSync(path.join(webDir, "dist", "index.html"), "utf8")
  const serviceWorker = readFileSync(path.join(webDir, "dist", "sw.js"), "utf8")
  const entryMatch = indexHtml.match(/src="\/(assets\/index-[^"]+\.js)"/)

  assert.ok(entryMatch, "could not find built app entry in dist/index.html")
  assert.match(
    serviceWorker,
    new RegExp(entryMatch[1].replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    `service worker precache is missing ${entryMatch[1]}`,
  )
})

test("i18n boot loads locale files lazily instead of bundling them all up front", () => {
  const source = readFileSync(path.join(webDir, "src", "i18n", "index.ts"), "utf8")

  assert.match(source, /import\.meta\.glob(?:<[^>]+>)?\(/, "expected import.meta.glob-based locale loading")
  assert.doesNotMatch(source, /from "\.\/locales"/, "expected i18n bootstrap to stop importing all locale JSON eagerly")
})

test("initial language detection skips unsupported browser locales before falling back", () => {
  const config = readFileSync(path.join(webDir, "src", "i18n", "config.ts"), "utf8")
  const source = readFileSync(path.join(webDir, "src", "i18n", "index.ts"), "utf8")

  assert.match(config, /export function resolveSupportedLanguage\(/, "expected i18n config to expose a non-fallback locale resolver")
  assert.match(source, /const normalized = resolveSupportedLanguage\(candidate\)/, "expected initial language detection to distinguish unsupported candidates from fallback english")
  assert.doesNotMatch(source, /const normalized = normalizeLanguage\(candidate\)/, "expected initial language detection to stop normalizing browser candidates straight to fallback english")
})

test("app bootstrap still renders when i18n initialization rejects", () => {
  const source = readFileSync(path.join(webDir, "src", "main.tsx"), "utf8")

  assert.match(source, /i18nReady[\s\S]*\.catch\(/, "expected bootstrap to handle i18n startup failures")
  assert.match(source, /i18nReady[\s\S]*\.finally\(/, "expected bootstrap to render after i18n startup settles")
  assert.doesNotMatch(
    source,
    /i18nReady\.then\(\(\)\s*=>\s*\{\s*createRoot/,
    "expected app mount to stop depending on successful i18n startup",
  )
})

test("workflow dialog notify toggle uses i18n instead of hardcoded english", () => {
  const source = readFileSync(path.join(webDir, "src", "components", "instances", "preferences", "WorkflowDialog.tsx"), "utf8")

  assert.match(source, /tr\("workflowDialog\.fields\.notify"\)/, "expected workflow dialog notify label to use workflowDialog.fields.notify")
  assert.doesNotMatch(source, />Notify</, "expected workflow dialog to stop hardcoding Notify")
})

test("trackers table headers and empty state use i18n keys instead of hardcoded english", () => {
  const source = readFileSync(path.join(webDir, "src", "components", "torrents", "details", "TrackersTable.tsx"), "utf8")

  for (const key of [
    "trackersTable.columns.status",
    "trackersTable.columns.tracker",
    "trackersTable.columns.message",
    "trackersTable.columns.seeds",
    "trackersTable.columns.peers",
    "trackersTable.columns.leeches",
    "trackersTable.columns.downloads",
    "trackersTable.empty.noTrackers",
  ]) {
    assert.match(source, new RegExp(`tr\\("${key.replaceAll(".", "\\.")}"\\)`), `expected trackers table to use ${key}`)
  }

  for (const label of ["Status", "Tracker", "Message", "Seeds", "Peers", "Leeches", "DLs", "No trackers found"]) {
    assert.doesNotMatch(source, new RegExp(`"${label.replace(/[.*+?^${}()|[\]\\\\]/g, "\\$&")}"`), `expected trackers table to stop hardcoding ${label}`)
  }
})

test("relative-time helpers avoid hardcoded English phrasing", () => {
  const dateTimeUtils = readFileSync(path.join(webDir, "src", "lib", "dateTimeUtils.ts"), "utf8")
  const utils = readFileSync(path.join(webDir, "src", "lib", "utils.ts"), "utf8")
  const settings = readFileSync(path.join(webDir, "src", "pages", "Settings.tsx"), "utf8")

  assert.match(`${dateTimeUtils}\n${utils}`, /Intl\.RelativeTimeFormat/, "expected locale-aware relative-time formatting")
  assert.doesNotMatch(dateTimeUtils, /return "Just now"|return "Today"|return "Yesterday"/, "expected dateTimeUtils to stop hardcoding English relative labels")
  assert.match(settings, /Intl\.RelativeTimeFormat/, "expected settings relative-time formatting to use Intl.RelativeTimeFormat")
  assert.doesNotMatch(settings, /formatDuration\(Math\.abs\(secondsDiff\)\)/, "expected settings relative-time formatting to stop using english duration abbreviations")
})

test("settings application-info dates use the active i18n locale", () => {
  const settings = readFileSync(path.join(webDir, "src", "pages", "Settings.tsx"), "utf8")

  assert.match(settings, /function getApplicationInfoLocale\(\): string/, "expected settings to centralize the active app locale for application-info formatting")
  assert.match(settings, /toLocaleString\(getApplicationInfoLocale\(\), \{/, "expected application-info absolute dates to use the selected app locale")
  assert.match(settings, /new Intl\.RelativeTimeFormat\(getApplicationInfoLocale\(\), \{/, "expected application-info relative dates to use the selected app locale")
  assert.doesNotMatch(settings, /toLocaleString\(undefined, \{/, "expected settings to stop delegating application-info dates to the browser locale")
  assert.doesNotMatch(settings, /new Intl\.RelativeTimeFormat\(undefined, \{/, "expected settings to stop delegating relative times to the browser locale")
})

test("cross-seed memoized helper text depends on the translator", () => {
  const crossSeedPage = readFileSync(path.join(webDir, "src", "pages", "CrossSeedPage.tsx"), "utf8")

  for (const memoName of [
    "runButtonDisabledReason",
    "startSearchRunDisabledReason",
    "seededSearchIndexerPlaceholder",
    "seededSearchIndexerHelpText",
    "seededSearchGazelleStatus",
  ]) {
    assert.match(
      crossSeedPage,
      new RegExp(`${memoName} = useMemo\\([\\s\\S]*?\\}, \\[[^\\]]*\\btr\\b[^\\]]*\\]\\)`),
      `expected ${memoName} memo deps to include tr so helper text updates after language changes`,
    )
  }
})

test("query builder uses translation keys for field and operator labels", () => {
  const constants = readFileSync(path.join(webDir, "src", "components", "query-builder", "constants.ts"), "utf8")
  const fieldCombobox = readFileSync(path.join(webDir, "src", "components", "query-builder", "FieldCombobox.tsx"), "utf8")
  const leafCondition = readFileSync(path.join(webDir, "src", "components", "query-builder", "LeafCondition.tsx"), "utf8")

  assert.match(constants, /NAME:\s*\{\s*labelKey:/, "expected query-builder field metadata to define translation keys")
  assert.match(constants, /string:\s*\[\s*\{\s*value:\s*"EQUAL",\s*labelKey:/, "expected query-builder operator metadata to define translation keys")
  assert.doesNotMatch(fieldCombobox, /selectedField\?\.label|fieldDef\?\.label \?\? field|heading=\{group\.label\}/, "expected field combobox to render translated labels")
  assert.doesNotMatch(leafCondition, /\{op\.label\}/, "expected operator dropdown to render translated labels")
})
