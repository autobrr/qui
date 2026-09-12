import test from "node:test"
import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"

import { findLegacyPluralKeys } from "./check-legacy-plural-keys.mjs"

function writeLocales(t, locales) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-legacy-plural-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))

  for (const [locale, namespaces] of Object.entries(locales)) {
    fs.mkdirSync(path.join(root, locale), { recursive: true })
    for (const [namespace, data] of Object.entries(namespaces)) {
      fs.writeFileSync(path.join(root, locale, `${namespace}.json`), JSON.stringify(data))
    }
  }

  return root
}

test("reports _plural keys in every locale, English included", (t) => {
  const root = writeLocales(t, {
    en: { instances: { orphan: { filesCount_one: "{{count}} file", filesCount_plural: "{{count}} files" } } },
    cs: { instances: { orphan: { filesCount_plural: "{{count}} souboru" } } },
    de: { instances: { orphan: { filesCount_other: "{{count}} Dateien" } } },
  })

  assert.deepEqual(findLegacyPluralKeys(root), [
    "cs/instances.json: orphan.filesCount_plural",
    "en/instances.json: orphan.filesCount_plural",
  ])
})

test("accepts CLDR suffixes and keys that merely contain the word plural", (t) => {
  const root = writeLocales(t, {
    en: {
      common: {
        items_one: "{{count}} item",
        items_other: "{{count}} items",
        pluralRules: "Plural rules",
        legend_plural_hint: "Counts vary by language",
      },
    },
    cs: { common: { items_one: "{{count}} položka", items_few: "{{count}} položky", items_other: "{{count}} položek" } },
  })

  assert.deepEqual(findLegacyPluralKeys(root), [])
})
