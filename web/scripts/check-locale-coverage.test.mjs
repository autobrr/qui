import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"

const checker = new URL("./check-locale-coverage.mjs", import.meta.url)
const namespaces = fs.readdirSync(new URL("../src/i18n/locales/en/", import.meta.url))

for (const locale of ["fr", "de", "it", "ko", "pt-BR"]) {
  test(`${locale} coverage preserves errors and exceptions`, (t) => {
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

    const enPath = path.join(enRoot, "common.json")
    const localePath = path.join(localeRoot, "common.json")
    const english = {
      nested: { message: "<b>Hello {{name}}</b>", empty: "Value", missing: "Required" },
      items_one: "{{count}} item",
      items_other: "{{count}} items",
      legacy_plural: "{{count}} items{{plural}}",
      single_one: "One",
      tool: "qBittorrent",
      pathPlaceholder: "/path/to/files",
      review: "Untranslated sentence",
      extraVars: "Source",
    }
    const translated = {
      nested: { message: "<b>Bonjour {{name}}</b>", empty: "Valeur", missing: "Requis" },
      items_other: "{{count}} éléments",
      legacy_plural: "{{count}} éléments",
      single_one: "Un",
      single_other: "Plusieurs",
      tool: "qBittorrent",
      pathPlaceholder: "/path/to/files",
      review: "Untranslated sentence",
      extraVars: "Cible {{extra}}",
    }
    fs.writeFileSync(enPath, JSON.stringify(english))
    fs.writeFileSync(localePath, JSON.stringify(translated))

    function run(expectedStatus) {
      const result = spawnSync(process.execPath, [script, locale], { encoding: "utf8", timeout: 10_000 })
      assert.ifError(result.error)
      assert.equal(result.status, expectedStatus, result.stdout + result.stderr)
      return result.stdout
    }

    const valid = run(0)
    assert.match(valid, /Warnings: 2 \(untranslatedUnexplained: 1, untranslatedExplained: 1\)/)

    translated.nested = { message: "Bonjour", empty: "" }
    translated.extra = "Unexpected"
    delete translated.legacy_plural
    delete translated.single_one
    fs.writeFileSync(localePath, JSON.stringify(translated))
    const invalid = run(1)
    for (const message of [
      "[Missing Keys] 3 errors",
      "[Extra Keys] 1 error",
      "[Interpolation] 1 error",
      "[HTML Tags] 1 error",
      "[Empty Strings] 1 error",
    ]) {
      assert.ok(invalid.includes(message), invalid)
    }

    fs.writeFileSync(localePath, "\uFEFF{}")
    assert.match(run(1), /UTF-8 BOM detected/)
    fs.writeFileSync(localePath, "{")
    assert.match(run(1), /invalid JSON or encoding/)
    fs.writeFileSync(localePath, "{}")
    fs.writeFileSync(enPath, "{")
    assert.match(run(1), /failed to parse JSON, skipping checks/)
    fs.rmSync(enPath)
    assert.match(run(1), /English locale file missing/)
    fs.writeFileSync(enPath, "{}")
    fs.rmSync(localePath)
    assert.ok(run(1).includes(`${locale} locale file missing`))
    fs.rmSync(localeRoot, { recursive: true })
    assert.equal(run(0), `${locale} locale directory not found, skipping coverage check.\n`)
  })
}

test("rejects missing and unsupported locales", () => {
  for (const args of [[], ["cs"], ["../en"]]) {
    const result = spawnSync(process.execPath, [fileURLToPath(checker), ...args], { encoding: "utf8", timeout: 10_000 })
    assert.ifError(result.error)
    assert.equal(result.status, 1)
    assert.match(result.stderr, /Unknown locale/)
  }
})
