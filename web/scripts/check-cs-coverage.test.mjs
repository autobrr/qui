import test from "node:test"
import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"

const checker = new URL("./check-cs-coverage.mjs", import.meta.url)
const namespaces = fs.readdirSync(new URL("../src/i18n/locales/en/", import.meta.url))

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-cs-coverage-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))

  fs.mkdirSync(path.join(root, "scripts"))
  const script = path.join(root, "scripts", "check-cs-coverage.mjs")
  fs.copyFileSync(checker, script)

  const enRoot = path.join(root, "src", "i18n", "locales", "en")
  const csRoot = path.join(root, "src", "i18n", "locales", "cs")
  for (const directory of [enRoot, csRoot]) {
    fs.mkdirSync(directory, { recursive: true })
    for (const namespace of namespaces) {
      fs.writeFileSync(path.join(directory, namespace), "{}")
    }
  }

  return {
    write(english, czech) {
      fs.writeFileSync(path.join(enRoot, "common.json"), JSON.stringify(english))
      fs.writeFileSync(path.join(csRoot, "common.json"), JSON.stringify(czech))
    },
    run() {
      const result = spawnSync(process.execPath, [script], { encoding: "utf8", timeout: 10_000 })
      assert.ifError(result.error)
      return result
    },
  }
}

const english = {
  items_one: "{{count}} item",
  items_other: "{{count}} items",
}

test("requires a cs _few form for every English plural base", (t) => {
  const cs = fixture(t)
  cs.write(english, {
    items_one: "{{count}} položka",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Plural Forms] 1 error\n {2}- common\.items_few\n/)
})

test("a missing _one is reported once, by Missing Keys", (t) => {
  const cs = fixture(t)
  cs.write(english, {
    items_few: "{{count}} položky",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Missing Keys] 1 error\n {2}- common\.items_one:/)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})

test("accepts the three required forms and does not demand _many", (t) => {
  const cs = fixture(t)
  cs.write(english, {
    items_one: "{{count}} položka",
    items_few: "{{count}} položky",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})

test("keeps _many a valid extra key", (t) => {
  const cs = fixture(t)
  cs.write(english, {
    items_one: "{{count}} položka",
    items_few: "{{count}} položky",
    items_many: "{{count}} položky",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Extra Keys]/)
})

// An unsuffixed key is i18next's last-resort in-language lookup: it answers every
// category the locale omits, so a base carrying one needs no _few.
const englishWithCatchAll = { items: "{{count}} item", ...english }

test("an unsuffixed cs key covers the categories cs omits", (t) => {
  const cs = fixture(t)
  cs.write(englishWithCatchAll, {
    items: "{{count}} položek",
    items_one: "{{count}} položka",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})

test("the catch-all has to be on the cs side to count", (t) => {
  const cs = fixture(t)
  cs.write(englishWithCatchAll, {
    items_one: "{{count}} položka",
    items_other: "{{count}} položek",
  })

  const result = cs.run()
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Plural Forms] 1 error\n {2}- common\.items_few\n/)
})

test("ignores bases English does not pluralize", (t) => {
  const cs = fixture(t)
  cs.write({ label_other: "{{count}} items" }, { label_other: "{{count}} položek" })

  const result = cs.run()
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})
