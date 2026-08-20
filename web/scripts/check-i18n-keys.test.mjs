import test from "node:test"
import assert from "node:assert/strict"

import { collectMissingKeysForSource } from "./check-i18n-keys-lib.mjs"

test("uses namespace overrides and explicit namespace prefixes before reporting missing keys", () => {
  const source = `
    import { useTranslation } from "react-i18next"

    export function Example() {
      const { t, i18n } = useTranslation("instances")

      return (
        <div>
          <button>{t("common:actions.cancel")}</button>
          <button>{t("header.instanceSettings", { ns: "common" })}</button>
          <button>{i18n.t("actions.close", { ns: "common" })}</button>
          <button>{t("preferences.workflowsOverview.unknownError")}</button>
          <button>{t("preferences.workflowsOverview.unknownError")}</button>
        </div>
      )
    }
  `

  const locales = {
    common: {
      actions: {
        cancel: "Cancel",
        close: "Close",
      },
      header: {
        instanceSettings: "Instance settings",
      },
    },
    instances: {
      preferences: {
        workflowsOverview: {},
      },
    },
  }

  const missingKeys = collectMissingKeysForSource({
    source,
    relativePath: "src/example.tsx",
    loadLocale(namespace) {
      return locales[namespace] ?? null
    },
  })

  assert.deepEqual(missingKeys, [
    "src/example.tsx: instances.preferences.workflowsOverview.unknownError",
  ])
})

test("supports i18n.t calls that use an explicit namespace prefix", () => {
  const source = `
    import { useTranslation } from "react-i18next"

    export function Example() {
      const { i18n } = useTranslation("torrents")

      return <button>{i18n.t("common:actions.search")}</button>
    }
  `

  const locales = {
    common: {
      actions: {
        search: "Search",
      },
    },
    torrents: {},
  }

  const missingKeys = collectMissingKeysForSource({
    source,
    relativePath: "src/example.tsx",
    loadLocale(namespace) {
      return locales[namespace] ?? null
    },
  })

  assert.deepEqual(missingKeys, [])
})
