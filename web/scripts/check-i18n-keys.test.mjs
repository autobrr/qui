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

const keyPropertyLocales = {
  common: {
    nav: { dashboard: "Dashboard" },
  },
  torrents: {
    columns: { name: "Name" },
    page: { routeTitle: "Torrents" },
  },
}

function collectKeyPropertyErrors(source) {
  return collectMissingKeysForSource({
    source,
    relativePath: "src/example.tsx",
    loadLocale(namespace) {
      return keyPropertyLocales[namespace] ?? null
    },
  })
}

for (const property of ["labelKey", "titleKey", "placeholderKey", "descriptionKey"]) {
  test(`reports a misspelled ${property} in the useTranslation namespace`, () => {
    const source = `
      const items = [
        { id: "a", ${property}: "columns.name" },
        { id: "b", ${property}: "columns.nmae" },
      ]

      export function Example() {
        const { t } = useTranslation("torrents")
        return items.map((item) => t(item.${property}))
      }
    `

    assert.deepEqual(collectKeyPropertyErrors(source), [
      "src/example.tsx: torrents.columns.nmae",
    ])
  })
}

test("checks key properties in a data-only file against its directive namespace", () => {
  const source = `
    // i18n-namespace: torrents
    export const options = [
      { value: "name", labelKey: "columns.name" },
      { value: "size", labelKey: "columns.szie" },
    ]
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [
    "src/example.tsx: torrents.columns.szie",
  ])
})

test("reports a key that exists only in another namespace", () => {
  const source = `
    // i18n-namespace: torrents
    export const items = [{ labelKey: "nav.dashboard" }]
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [
    "src/example.tsx: torrents.nav.dashboard",
  ])
})

test("checks a route titleKey against its sibling titleNs", () => {
  const source = `
    export const Route = createFileRoute("/example")({
      staticData: {
        titleKey: "page.routeTitle",
        titleNs: "torrents",
      },
    })

    function Example() {
      const { t } = useTranslation(["common", "torrents"])
      return t("nav.dashboard")
    }
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [])
  assert.deepEqual(collectKeyPropertyErrors(source.replace("page.routeTitle", "page.routeTitel")), [
    "src/example.tsx: torrents.page.routeTitel",
  ])
})

test("reports key properties in a file with no namespace", () => {
  const source = `
    export const options = [{ value: "name", labelKey: "columns.name" }]
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [
    "src/example.tsx: key properties have no namespace; add \"// i18n-namespace: <ns>\" to the file",
  ])
})

test("reports a directive in a file that calls useTranslation", () => {
  const source = `
    // i18n-namespace: torrents
    export function Example() {
      const { t } = useTranslation("torrents")
      return t("columns.name")
    }
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [
    "src/example.tsx: remove \"// i18n-namespace: torrents\"; useTranslation sets the namespace",
  ])
})

test("ignores commented-out key properties", () => {
  const source = `
    // i18n-namespace: torrents
    export const options = [
      { value: "name", labelKey: "columns.name" },
      // { value: "size", labelKey: "columns.szie" },
    ]
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [])
})

test("resolves a namespace prefix in a key property", () => {
  const source = `
    // i18n-namespace: torrents
    export const items = [
      { labelKey: "common:nav.dashboard" },
      { labelKey: "common:nav.dashbaord" },
    ]
  `

  assert.deepEqual(collectKeyPropertyErrors(source), [
    "src/example.tsx: common.nav.dashbaord",
  ])
})
