import test from "node:test"
import assert from "node:assert/strict"

const detectorModule = await import("./find-hardcoded-i18n-literals.mjs")

test("flags JSX text nodes and UI string properties", () => {
  const source = `
    import { Button } from "@/components/ui/button"

    export function Example() {
      const actions = [
        { label: "Dashboard", description: "Main overview" },
      ]

      return (
        <div>
          <p>Loading torrents...</p>
          <Button title="Open details">Open</Button>
          <input placeholder="Search torrents" />
          {actions.map((action) => (
            <span key={action.label}>{action.label}</span>
          ))}
        </div>
      )
    }
  `

  const matches = detectorModule.findHardcodedStringsInSource(source, "src/example.tsx")

  assert.deepEqual(
    matches.map((match) => match.text),
    [
      "Dashboard",
      "Main overview",
      "Loading torrents...",
      "Open details",
      "Open",
      "Search torrents",
    ],
  )
})

test("ignores translation calls, non-UI attributes, and code spans", () => {
  const source = `
    import { useTranslation } from "react-i18next"

    export function Example() {
      const { t } = useTranslation("common")
      const items = [{ value: "strict" }]

      return (
        <div className="space-y-2" data-testid="example">
          <p>{t("common.loading")}</p>
          <code>THEMES_REPO_TOKEN</code>
          <span>{items[0].value}</span>
          <Button aria-label={t("common.close")} />
        </div>
      )
    }
  `

  const matches = detectorModule.findHardcodedStringsInSource(source, "src/example.tsx")

  assert.deepEqual(matches, [])
})

test("flags UI copy assigned through interesting variable names", () => {
  const source = `
    export function Example() {
      const title = "Create backup"
      const helperText = "Runs every night"
      const slug = "nightly-job"

      return <section aria-label={title}>{helperText}</section>
    }
  `

  const matches = detectorModule.findHardcodedStringsInSource(source, "src/example.tsx")

  assert.deepEqual(
    matches.map((match) => [match.kind, match.text]),
    [
      ["variable", "Create backup"],
      ["variable", "Runs every night"],
    ],
  )
})

test("flags UI string properties in .ts option tables, including reason", () => {
  const source = `
    export const SORT_OPTIONS = [{ value: "added_on", label: "Recently Added" }]
    export const DISABLED = [{ field: "NAME", reason: "Not supported here" }]
    const title = "Not a property"
  `

  const matches = detectorModule.findHardcodedStringsInSource(source, "src/lib/options.ts")

  assert.deepEqual(
    matches.map((match) => [match.kind, match.text]),
    [
      ["object-property", "Recently Added"],
      ["object-property", "Not supported here"],
    ],
  )
})

test("skips only the named fallback tables in the query-builder constants", () => {
  const source = `
    export const TORRENT_STATES = [{ value: "downloading", label: "Downloading" }]
    export const NEW_TABLE = [{ value: "x", label: "Rendered raw" }]
  `

  for (const filePath of ["src/components/query-builder/constants.ts", "C:\\qui\\web\\src\\components\\query-builder\\constants.ts"]) {
    const matches = detectorModule.findHardcodedStringsInSource(source, filePath)
    assert.deepEqual(matches.map((match) => match.text), ["Rendered raw"])
  }

  const elsewhere = detectorModule.findHardcodedStringsInSource(source, "src/lib/constants.ts")
  assert.deepEqual(elsewhere.map((match) => match.text), ["Downloading", "Rendered raw"])
})
