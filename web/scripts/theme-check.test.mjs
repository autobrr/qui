import test from "node:test"
import assert from "node:assert/strict"
import fs from "node:fs"
import path from "node:path"

import { builtInThemeFiles, checkTheme, declarations } from "./theme-check.mjs"

const WHITE_SURFACES = ["background", "card", "popover", "muted", "secondary", "accent", "sidebar", "sidebar-accent"]
const BLACK_FILLS = [
  "primary", "destructive", "sidebar-primary", "ring", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
  "success", "warning", "info",
]
const BLACK_TEXT = [
  "foreground", "card-foreground", "popover-foreground", "muted-foreground", "secondary-foreground", "accent-foreground",
  "sidebar-foreground", "sidebar-accent-foreground",
]
const WHITE_TEXT = ["primary-foreground", "destructive-foreground", "sidebar-primary-foreground"]

function theme(tokens = {}, header = "") {
  const values = {
    ...Object.fromEntries(WHITE_SURFACES.map(name => [name, "#ffffff"])),
    ...Object.fromEntries(BLACK_FILLS.map(name => [name, "#000000"])),
    ...Object.fromEntries(BLACK_TEXT.map(name => [name, "#000000"])),
    ...Object.fromEntries(WHITE_TEXT.map(name => [name, "#ffffff"])),
    ...tokens,
  }
  const body = Object.entries(values).map(([name, value]) => `  --${name}: ${value};`).join("\n")
  return `/* @name: Fixture\n${header} */\n:root {\n${body}\n}\n\n.dark {\n${body}\n}\n`
}

const failed = result => result.pairs.filter(pair => !pair.ok)
const pair = (result, label, mode = "light") => result.pairs.find(entry => entry.label === label && entry.mode === mode)

test("a theme of black on white passes each pair", () => {
  const result = checkTheme(theme())

  assert.deepEqual(failed(result), [])
  assert.deepEqual(result.outsideSrgb, [])
})

test("follows var() and checks each theme variation", () => {
  const result = checkTheme(theme({
    "variation-dark": "#000000",
    "variation-pale": "#dddddd",
    "variation-color": "var(--variation-dark)",
    primary: "var(--variation-color)",
  }, " * @variations: dark, pale\n"))

  assert.deepEqual([...new Set(failed(result).map(entry => entry.variation))], ["pale"])
  assert.ok(failed(result).some(entry => entry.label === "primary text on background"))
})

test("composes a color that has its own alpha", () => {
  const result = checkTheme(theme({ "muted-foreground": "oklch(0 0 0 / 50%)" }))

  const entry = pair(result, "muted-foreground on background")
  assert.equal(entry.colors, "#808080 on #ffffff")
  assert.equal(entry.ok, false)
})

test("blends the rows as the torrent table does", () => {
  const result = checkTheme(theme({ muted: "#000000", accent: "#ff0000" }))

  assert.equal(pair(result, "foreground text on striped row").colors, "#000000 on #999999")
  assert.equal(pair(result, "foreground text on hovered row").colors, "#000000 on #ff9999")
  assert.equal(pair(result, "foreground text on selected row").colors, "#000000 on #ff0000")
})

test("the destructive menu item uses 10% in light mode and 20% in dark mode", () => {
  const result = checkTheme(theme({ destructive: "#ff0000" }))

  assert.ok(pair(result, "destructive text on destructive 10% on popover (focused menu item)", "light"))
  assert.ok(pair(result, "destructive text on destructive 20% on popover (focused menu item)", "dark"))
})

test("checks a status color on the page, a card, a popover, its own tint, and each torrent row", () => {
  const result = checkTheme(theme({ warning: "#dddddd" }))

  assert.deepEqual(failed(result).filter(entry => entry.mode === "light").map(entry => entry.label), [
    "warning text on background",
    "warning text on card",
    "warning text on popover",
    "warning text on warning 10% on background",
    "warning text on row",
    "warning text on striped row",
    "warning text on hovered row",
    "warning text on selected row",
  ])
})

test("checks the foreground text of a kind chip on each chart tint, on the page and on a card", () => {
  const result = checkTheme(theme({
    foreground: "#767676",
    "chart-1": "#ffffff", "chart-2": "#ffffff", "chart-3": "#ffffff", "chart-4": "#000000", "chart-5": "#ffffff",
  }))

  assert.deepEqual(
    failed(result).filter(entry => entry.mode === "light" && entry.label.startsWith("foreground on chart-")).map(entry => entry.label),
    ["foreground on chart-4 10% on background", "foreground on chart-4 10% on card"],
  )
})

test("checks the chart text of the log viewer values on the muted box over the page", () => {
  const result = checkTheme(theme({ card: "#000000", muted: "#000000", "chart-text-2": "#ffffff", "chart-text-3": "#000000" }))

  assert.deepEqual(
    result.pairs.filter(entry => entry.mode === "light" && entry.label.endsWith("on muted 30% on background")).map(entry => [entry.label, entry.ok]),
    [["chart-text-2 text on muted 30% on background", false], ["chart-text-3 text on muted 30% on background", true]],
  )
})

test("each built-in theme keeps the label of a kind chip readable on each chart tint", () => {
  for (const file of builtInThemeFiles()) {
    const faint = checkTheme(fs.readFileSync(file, "utf8")).pairs
      .filter(pair => !pair.ok && /^foreground on chart-\d 10% on /.test(pair.label))
      .map(pair => `${pair.mode}${pair.variation ? ` ${pair.variation}` : ""} ${pair.label}`)
    assert.deepEqual(faint, [], path.basename(file))
  }
})

test("checks a theme without status colors with the standard ones", () => {
  const fixture = theme().replace(/^ {2}--(success|warning|info):.*\n/gm, "")

  assert.equal(pair(checkTheme(fixture), "warning text on background").colors, "#bb4d00 on #ffffff")
  assert.equal(pair(checkTheme(fixture), "warning text on background", "dark").colors, "#ffb900 on #ffffff")
})

test("checks chart text with the theme's chart text color, or with its chart color when it sets none", () => {
  const faintChart = { "chart-2": "#dddddd" }

  assert.equal(pair(checkTheme(theme(faintChart)), "chart-text-2 text on row").ok, false)
  assert.equal(pair(checkTheme(theme({ ...faintChart, "chart-text-2": "#000000" })), "chart-text-2 text on row").ok, true)
})

test("reports a color that sRGB cannot show", () => {
  const result = checkTheme(theme({ primary: "oklch(0.82 0.18 100)" }))

  assert.deepEqual(result.outsideSrgb, ["light --primary: oklch(0.82 0.18 100)", "dark --primary: oklch(0.82 0.18 100)"])
})

const KNOWN_OUTSIDE_SRGB = {
  "dragon.css": ["light --variation-pink: oklch(0.6931 0.1971 22.11)"],
  "minimal.css": [
    "light --destructive: oklch(0.5770 0.2450 27.3250)",
    "dark --destructive: oklch(0.7040 0.1910 22.2160)",
  ],
  "napster.css": [
    "light --accent: oklch(0.82 0.18 100)",
    "light --chart-2: oklch(0.82 0.18 100)",
    "dark --accent: oklch(0.82 0.18 100)",
    "dark --chart-2: oklch(0.82 0.18 100)",
  ],
  "wave.css": ["light --variation-pink: oklch(0.6931 0.1971 22.11)"],
}

test("each built-in theme keeps its colors inside sRGB", () => {
  const files = builtInThemeFiles()
  assert.ok(files.length > 0)

  for (const file of files) {
    const name = path.basename(file)
    assert.deepEqual(checkTheme(fs.readFileSync(file, "utf8")).outsideSrgb, KNOWN_OUTSIDE_SRGB[name] ?? [], name)
  }
})

test("each community theme keeps its ratio colors readable on the plain and the striped row", () => {
  const files = builtInThemeFiles().filter(file => path.basename(path.dirname(file)) === "premium")
  assert.ok(files.length > 0)

  for (const file of files) {
    const faint = checkTheme(fs.readFileSync(file, "utf8")).pairs
      .filter(pair => !pair.ok && /^chart-text-\d text on (row|striped row)$/.test(pair.label))
      .map(pair => `${pair.mode} ${pair.label}`)
    assert.deepEqual(faint, [], path.basename(file))
  }
})

test("each built-in theme sets its own status colors in light and dark", () => {
  const files = builtInThemeFiles()
  assert.ok(files.length > 0)

  for (const file of files) {
    const css = fs.readFileSync(file, "utf8")
    const missing = [["light", ":root"], ["dark", ".dark"]].flatMap(([mode, selector]) => {
      const vars = declarations(css, selector)
      return ["--success", "--warning", "--info"].filter(name => !vars[name]).map(name => `${mode} ${name}`)
    })
    assert.deepEqual(missing, [], path.basename(file))
  }
})

const SELECTED_ROW_TOO_BRIGHT = ["success", "warning", "info"].map(name => `dark ${name} text on selected row`)
const ACCEPTED_STATUS_PAIRS = {
  "bubblegum.css": SELECTED_ROW_TOO_BRIGHT,
  "cyberpunk.css": SELECTED_ROW_TOO_BRIGHT,
  "nord.css": SELECTED_ROW_TOO_BRIGHT,
}

test("each built-in theme keeps its status colors readable, except the pairs that the maintainer accepted", () => {
  for (const file of builtInThemeFiles()) {
    const name = path.basename(file)
    const faint = checkTheme(fs.readFileSync(file, "utf8")).pairs
      .filter(pair => !pair.ok && /^(success|warning|info) text on /.test(pair.label))
      .map(pair => `${pair.mode} ${pair.label}`)
    assert.deepEqual([...new Set(faint)], ACCEPTED_STATUS_PAIRS[name] ?? [], name)
  }
})
