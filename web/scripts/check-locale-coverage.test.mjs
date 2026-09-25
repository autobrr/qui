import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"

const checker = new URL("./check-locale-coverage.mjs", import.meta.url)
const namespaces = fs.readdirSync(new URL("../src/i18n/locales/en/", import.meta.url))
const allLocales = ["fr", "de", "it", "ko", "pt-BR", "ca", "cs", "uk", "zh-CN", "zh-TW"]

function fixture(t, locale) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-locale-coverage-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))

  fs.mkdirSync(path.join(root, "scripts"))
  const script = path.join(root, "scripts", "check-locale-coverage.mjs")
  fs.copyFileSync(checker, script)

  const enRoot = path.join(root, "src", "i18n", "locales", "en")
  const localeRoot = path.join(root, "src", "i18n", "locales", locale)
  for (const directory of [enRoot, localeRoot]) {
    fs.mkdirSync(directory, { recursive: true })
    for (const namespace of namespaces) {
      fs.writeFileSync(path.join(directory, namespace), "{}")
    }
  }

  return {
    enRoot,
    localeRoot,
    write(english, translated) {
      fs.writeFileSync(path.join(enRoot, "common.json"), typeof english === "string" ? english : JSON.stringify(english))
      fs.writeFileSync(path.join(localeRoot, "common.json"), typeof translated === "string" ? translated : JSON.stringify(translated))
    },
    run(...args) {
      const result = spawnSync(process.execPath, [script, ...args], { encoding: "utf8", timeout: 10_000 })
      assert.ifError(result.error)
      return result
    },
  }
}

// English pluralizes with _one/_other; every locale-specific rule keys off that pair.
const english = { items_one: "{{count}} item", items_other: "{{count}} items" }

for (const locale of allLocales) {
  test(`${locale}: shared checks report against English`, (t) => {
    const f = fixture(t, locale)
    f.write(
      { nested: { message: "<b>Hello {{name}}</b>", empty: "Value", missing: "Required" }, savePath: "/path/to/files", review: "Untranslated sentence" },
      { nested: { message: "Bonjour", empty: "" }, extra: "Unexpected", savePath: "/path/to/files", review: "Untranslated sentence" },
    )

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    for (const message of [
      "[Missing Keys] 1 error",
      "[Extra Keys] 1 error",
      "[Interpolation] 1 error",
      "[HTML Tags] 1 error",
      "[Empty Strings] 1 error",
      "[Untranslated (needs review)] 1 warning",
    ]) {
      assert.ok(result.stdout.includes(message), result.stdout)
    }
    // Every locale gets untranslated reporting, uk included: before the checkers were merged, uk
    // was the one locale with 116 English-identical values and no warning for any of them.
    assert.match(result.stdout, /Untranslated \(kept intentionally[^\]]*\)] 1 warning/)
  })

  test(`${locale}: an extra placeholder is an error`, (t) => {
    const f = fixture(t, locale)
    f.write({ m: "Hello" }, { m: "Bonjour {{name}}" })

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Interpolation] 1 error\n {2}- common\.m: extra \{\{name}} not in en\n/)
  })

  test(`${locale}: {{plural}} is English grammar and may be dropped`, (t) => {
    const f = fixture(t, locale)
    f.write({ m: "{{count}} item{{plural}}" }, { m: "{{count}} x" })

    assert.equal(f.run(locale).status, 0)
  })

  test(`${locale}: a missing locale directory fails`, (t) => {
    const f = fixture(t, locale)
    fs.rmSync(f.localeRoot, { recursive: true })

    const result = f.run(locale)
    assert.equal(result.status, 1)
    assert.match(result.stderr, /locale directory not found/)
  })

  test(`${locale}: encoding problems are reported`, (t) => {
    const f = fixture(t, locale)

    f.write({}, "﻿{}")
    assert.match(f.run(locale).stdout, /UTF-8 BOM detected/)

    f.write({}, "{")
    assert.match(f.run(locale).stdout, /invalid JSON or encoding/)

    f.write("{", {})
    assert.match(f.run(locale).stdout, /failed to parse JSON, skipping checks/)

    f.write({}, {})
    fs.rmSync(path.join(f.enRoot, "common.json"))
    assert.match(f.run(locale).stdout, /English locale file missing/)

    fs.writeFileSync(path.join(f.enRoot, "common.json"), "{}")
    fs.rmSync(path.join(f.localeRoot, "common.json"))
    assert.ok(f.run(locale).stdout.includes(`${locale} locale file missing`))
  })
}

// --- plural categories -------------------------------------------------------
// Configured per locale, never from CLDR: i18next resolves a category a locale lacks against
// English, and the categories that are actually reachable are narrower than CLDR lists.

for (const locale of ["fr", "de", "it", "pt-BR", "ca", "cs", "uk"]) {
  test(`${locale}: _one is required`, (t) => {
    const f = fixture(t, locale)
    f.write(english, { items_other: "{{count}} x", items_few: "{{count}} f", items_many: "{{count}} m" })

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Missing Keys] 1 error\n {2}- common\.items_one:/)
  })
}

for (const locale of ["ko", "zh-CN", "zh-TW"]) {
  test(`${locale}: _one may be omitted`, (t) => {
    const f = fixture(t, locale)
    f.write(english, { items_other: "{{count}} x" })

    assert.equal(f.run(locale).status, 0)
  })

  test(`${locale}: a present _one is a warning, not an error`, (t) => {
    const f = fixture(t, locale)
    f.write(english, { items_one: "{{count}} x", items_other: "{{count}} y" })

    const result = f.run(locale)
    assert.equal(result.status, 0, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Plural Forms] 1 warning\n {2}- common\.items_one: /)
  })
}

test("cs requires _few for every English plural base", (t) => {
  const f = fixture(t, "cs")
  f.write(english, { items_one: "{{count}} položka", items_other: "{{count}} položek" })

  const result = f.run("cs")
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Plural Forms] 1 error\n {2}- common\.items_few\n/)
})

test("cs accepts the three required forms and does not demand _many", (t) => {
  const f = fixture(t, "cs")
  f.write(english, { items_one: "{{count}} položka", items_few: "{{count}} položky", items_other: "{{count}} položek" })

  const result = f.run("cs")
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})

test("cs keeps _many a valid extra key", (t) => {
  const f = fixture(t, "cs")
  f.write(english, {
    items_one: "{{count}} položka",
    items_few: "{{count}} položky",
    items_many: "{{count}} položky",
    items_other: "{{count}} položek",
  })

  const result = f.run("cs")
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Extra Keys]/)
})

test("uk requires _few and _many", (t) => {
  const f = fixture(t, "uk")
  f.write(english, { items_one: "{{count}} елемент", items_other: "{{count}} елементів" })

  const result = f.run("uk")
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Plural Forms] 2 errors\n {2}- common\.items_few\n {2}- common\.items_many\n/)
})

test("a missing _one is reported once, by Missing Keys", (t) => {
  const f = fixture(t, "uk")
  f.write(english, { items_few: "{{count}} f", items_many: "{{count}} m", items_other: "{{count}} o" })

  const result = f.run("uk")
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /\[Missing Keys] 1 error\n {2}- common\.items_one:/)
  assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
})

// An unsuffixed key is i18next's last-resort in-language lookup: it answers every category the
// locale omits, so a base carrying one needs no _few or _many.
const englishWithCatchAll = { items: "{{count}} item", ...english }

for (const locale of ["cs", "uk"]) {
  test(`${locale}: an unsuffixed key covers the categories the locale omits`, (t) => {
    const f = fixture(t, locale)
    f.write(englishWithCatchAll, { items: "{{count}} x", items_one: "{{count}} y", items_other: "{{count}} z" })

    const result = f.run(locale)
    assert.equal(result.status, 0, result.stdout + result.stderr)
    assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
  })

  test(`${locale}: the catch-all has to be on the locale side to count`, (t) => {
    const f = fixture(t, locale)
    f.write(englishWithCatchAll, { items_one: "{{count}} y", items_other: "{{count}} z" })

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Plural Forms] \d errors?\n {2}- common\.items_few\n/)
  })

  test(`${locale}: placeholders and markup are checked on locale-only plural forms`, (t) => {
    const f = fixture(t, locale)
    f.write(
      { items_one: "<b>{{count}}</b> item", items_other: "<b>{{count}}</b> items" },
      { items_one: "<b>{{count}}</b> y", items_few: "few", items_many: "<i>{{count}}</i> m", items_other: "<b>{{count}}</b> z" },
    )

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Interpolation] 1 error\n {2}- common\.items_few: missing \{\{count}}/)
    assert.match(result.stdout, /\[HTML Tags] \d errors?\n {2}- common\.items_few: missing <b>/)
  })

  test(`${locale}: ignores bases English does not pluralize`, (t) => {
    const f = fixture(t, locale)
    f.write({ label_other: "{{count}} items" }, { label_other: "{{count}} x" })

    const result = f.run(locale)
    assert.equal(result.status, 0, result.stdout + result.stderr)
    assert.doesNotMatch(result.stdout, /\[Plural Forms]/)
  })

  test(`${locale}: a _few key on a base English does not pluralize is an extra key`, (t) => {
    const f = fixture(t, locale)
    f.write({ label_other: "{{count}} items" }, { label_other: "{{count}} x", label_few: "{{count}} f" })

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Extra Keys] 1 error\n {2}- common\.label_few\n/)
  })
}

for (const locale of ["fr", "zh-CN"]) {
  test(`${locale}: _few is not a category this locale may carry`, (t) => {
    const f = fixture(t, locale)
    f.write(english, { items_one: "{{count}} x", items_other: "{{count}} y", items_few: "{{count}} f" })

    const result = f.run(locale)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Extra Keys] 1 error\n {2}- common\.items_few\n/)
  })
}

// --- Chinese-only checks -----------------------------------------------------

for (const locale of ["zh-CN", "zh-TW"]) {
  test(`${locale}: half-width punctuation is a warning`, (t) => {
    const f = fixture(t, locale)
    f.write({ q: "Hello, world?" }, { q: "你好,世界?" })

    const result = f.run(locale)
    assert.equal(result.status, 0, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Punctuation] 2 warnings/)
  })

  test(`${locale}: a much longer translation is a warning`, (t) => {
    const f = fixture(t, locale)
    f.write({ l: "Short text" }, { l: "这是一个非常非常非常非常长长长长长长的句子" })

    const result = f.run(locale)
    assert.equal(result.status, 0, result.stdout + result.stderr)
    assert.match(result.stdout, /\[Text Length] 1 warning/)
  })
}

test("punctuation and length are not checked outside the Chinese locales", (t) => {
  const f = fixture(t, "fr")
  f.write({ q: "Hello, world?", l: "Short text" }, { q: "你好,世界?", l: "这是一个非常非常非常非常长长长长长长的句子" })

  const result = f.run("fr")
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.doesNotMatch(result.stdout, /\[Punctuation]|\[Text Length]/)
})

// --- invocation --------------------------------------------------------------

test("rejects unsupported locales", () => {
  for (const argument of ["en", "xx", "../en"]) {
    const result = spawnSync(process.execPath, [fileURLToPath(checker), argument], { encoding: "utf8", timeout: 10_000 })
    assert.ifError(result.error)
    assert.equal(result.status, 1)
    assert.match(result.stderr, /Unknown locale/)
  }
})

test("without an argument every configured locale is checked in one process", (t) => {
  const f = fixture(t, "fr")
  for (const locale of allLocales) {
    const localeRoot = path.join(f.localeRoot, "..", locale)
    fs.mkdirSync(localeRoot, { recursive: true })
    for (const namespace of namespaces) {
      fs.writeFileSync(path.join(localeRoot, namespace), "{}")
    }
  }

  const passing = f.run()
  assert.equal(passing.status, 0, passing.stdout + passing.stderr)
  for (const locale of allLocales) {
    assert.ok(passing.stdout.includes(`${locale} translation coverage: all checks passed.`), `no report for ${locale}`)
  }

  // One bad locale fails the whole run, so a single invocation can replace the per-locale ones.
  fs.writeFileSync(path.join(f.localeRoot, "..", "ca", "common.json"), JSON.stringify({ extra: "Unexpected" }))
  const failing = f.run()
  assert.equal(failing.status, 1, failing.stdout)
  assert.match(failing.stdout, /=== ca Translation Coverage Report ===/)
})
