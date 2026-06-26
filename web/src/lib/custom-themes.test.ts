/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect } from "vitest"
import { parseCustomThemes, isCustomThemeId, CUSTOM_THEME_PREFIX } from "./custom-themes"

const validCss = `/* @name: Ocean
 * @description: Calm blue
 * @lightOnly: true
 */
:root { --primary: oklch(0.5 0.1 250); --background: #ffffff; }
.dark { --primary: oklch(0.6 0.1 250); --background: #000000; }
`

const missingName = `/* @description: no name here */
:root { --primary: red; --background: #fff; }
.dark { --primary: darkred; --background: #111; }
`

// Missing the required .dark block - parseThemeCSS returns null.
const invalidCss = `/* @name: Broken */
:root { --primary: red; }
`

describe("parseCustomThemes", () => {
  it("maps a valid file to a prefixed custom theme", () => {
    const { themes, errors } = parseCustomThemes([{ id: "ocean", filename: "ocean.css", css: validCss }])

    expect(errors).toHaveLength(0)
    expect(themes).toHaveLength(1)

    const theme = themes[0]
    expect(theme.id).toBe(`${CUSTOM_THEME_PREFIX}ocean`)
    expect(theme.name).toBe("Ocean")
    expect(theme.description).toBe("Calm blue")
    expect(theme.lightOnly).toBe(true)
    expect(theme.isCustom).toBe(true)
    expect(theme.isPremium).toBe(true)
    expect(theme.rawCss).toBe(validCss)
    expect(theme.cssVars.light["--primary"]).toBe("oklch(0.5 0.1 250)")
    expect(theme.cssVars.dark["--background"]).toBe("#000000")
    // Custom themes never carry variations.
    expect(theme.variations).toBeUndefined()
  })

  it("collects unparseable files as errors without throwing", () => {
    const { themes, errors } = parseCustomThemes([
      { id: "ocean", filename: "ocean.css", css: validCss },
      { id: "broken", filename: "broken.css", css: invalidCss },
    ])

    expect(themes).toHaveLength(1)
    expect(themes[0].id).toBe(`${CUSTOM_THEME_PREFIX}ocean`)
    expect(errors).toEqual([{ filename: "broken.css" }])
  })

  it("falls back to a default name when @name is absent", () => {
    const { themes } = parseCustomThemes([{ id: "x", filename: "x.css", css: missingName }])
    expect(themes[0].name).toBe("Untitled Theme")
  })

  it("returns empty results for an empty list", () => {
    expect(parseCustomThemes([])).toEqual({ themes: [], errors: [] })
  })
})

describe("isCustomThemeId", () => {
  it("recognizes custom ids", () => {
    expect(isCustomThemeId("custom:ocean")).toBe(true)
  })

  it("rejects built-in and empty ids", () => {
    expect(isCustomThemeId("minimal")).toBe(false)
    expect(isCustomThemeId(null)).toBe(false)
    expect(isCustomThemeId(undefined)).toBe(false)
    expect(isCustomThemeId("")).toBe(false)
  })
})
