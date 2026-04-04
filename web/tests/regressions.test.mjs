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
  "torrentDetailsPanel.count.files",
  "torrentDetailsPanel.count.sources",
  "torrentDetailsPanel.count.trackers",
  "torrentManagementBar.toolbarAria",
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

test("relative-time helpers avoid hardcoded English phrasing", () => {
  const dateTimeUtils = readFileSync(path.join(webDir, "src", "lib", "dateTimeUtils.ts"), "utf8")
  const utils = readFileSync(path.join(webDir, "src", "lib", "utils.ts"), "utf8")

  assert.match(`${dateTimeUtils}\n${utils}`, /Intl\.RelativeTimeFormat/, "expected locale-aware relative-time formatting")
  assert.doesNotMatch(dateTimeUtils, /return "Just now"|return "Today"|return "Yesterday"/, "expected dateTimeUtils to stop hardcoding English relative labels")
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
