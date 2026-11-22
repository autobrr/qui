import test from "node:test"
import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"

import { findLocaleBackticks } from "./check-locale-backticks.mjs"

function writeLocales(t, locales) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-locale-backticks-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))

  for (const [locale, namespaces] of Object.entries(locales)) {
    fs.mkdirSync(path.join(root, locale), { recursive: true })
    for (const [namespace, data] of Object.entries(namespaces)) {
      fs.writeFileSync(path.join(root, locale, `${namespace}.json`), JSON.stringify(data))
    }
  }

  return root
}

test("reports each string with a backtick, nested keys and every locale included", (t) => {
  const root = writeLocales(t, {
    en: { settings: { hint: "Set `instance` in the widget", plain: "No code here" } },
    de: { settings: { nested: { hint: "Setze `instance`" } } },
  })

  assert.deepEqual(findLocaleBackticks(root), [
    "de/settings.json: nested.hint",
    "en/settings.json: hint",
  ])
})

test("accepts <code> markup and plain text", (t) => {
  const root = writeLocales(t, {
    en: { settings: { hint: "Set <code>instance</code> in the widget", count: "{{count}} keys" } },
  })

  assert.deepEqual(findLocaleBackticks(root), [])
})
