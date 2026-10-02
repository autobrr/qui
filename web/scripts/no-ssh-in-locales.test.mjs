import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

// #2917 deletes this test when it adds the SSH setup screen; until then no UI text may name SSH.
test("no locale string mentions SSH", () => {
  const localesRoot = new URL("../src/i18n/locales/", import.meta.url)
  const hits = []

  function walk(value, file, key) {
    if (typeof value === "string") {
      if (/\bssh\b/i.test(value)) hits.push(`${file} ${key}: ${value}`)
      return
    }
    for (const [childKey, child] of Object.entries(value)) {
      walk(child, file, key ? `${key}.${childKey}` : childKey)
    }
  }

  for (const locale of fs.readdirSync(localesRoot)) {
    for (const name of fs.readdirSync(new URL(`${locale}/`, localesRoot))) {
      const file = `${locale}/${name}`
      walk(JSON.parse(fs.readFileSync(new URL(file, localesRoot), "utf8")), file, "")
    }
  }

  assert.deepEqual(hits, [])
})
