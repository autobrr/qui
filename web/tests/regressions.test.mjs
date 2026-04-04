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
