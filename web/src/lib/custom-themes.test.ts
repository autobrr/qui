/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect } from "vitest"
import { parseCustomThemes, isCustomThemeId, decideCustomThemeAction, CUSTOM_THEME_PREFIX, type CustomThemeDecisionInput } from "./custom-themes"
import type { Theme } from "@/config/themes"

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

describe("decideCustomThemeAction", () => {
  const ocean: Theme = {
    id: "custom:ocean",
    name: "Ocean",
    isCustom: true,
    isPremium: true,
    rawCss: ":root{--primary:blue}",
    cssVars: { light: {}, dark: {} },
  }

  const base: CustomThemeDecisionInput = {
    isLoading: false,
    isError: false,
    hasPremiumAccess: true,
    hasData: true,
    storedThemeId: null,
    themes: [],
    appliedCustomCss: null,
  }

  it("does nothing while the license check is loading or errored", () => {
    expect(decideCustomThemeAction({ ...base, isLoading: true })).toBe("noop")
    expect(decideCustomThemeAction({ ...base, isError: true })).toBe("noop")
  })

  it("clears and downgrades a stored custom theme when premium is lost", () => {
    expect(
      decideCustomThemeAction({ ...base, hasPremiumAccess: false, storedThemeId: "custom:ocean" })
    ).toBe("clear-and-downgrade")
  })

  it("only clears (no downgrade) when not premium and the selection is built-in", () => {
    expect(
      decideCustomThemeAction({ ...base, hasPremiumAccess: false, storedThemeId: "minimal" })
    ).toBe("clear")
  })

  it("waits for data before registering when premium", () => {
    expect(decideCustomThemeAction({ ...base, hasData: false, storedThemeId: "custom:ocean" })).toBe("noop")
  })

  it("registers without applying when the selection is a built-in theme", () => {
    expect(decideCustomThemeAction({ ...base, storedThemeId: "minimal", themes: [ocean] })).toBe("register")
  })

  it("downgrades when the stored custom theme is gone", () => {
    expect(
      decideCustomThemeAction({ ...base, storedThemeId: "custom:gone", themes: [ocean] })
    ).toBe("register-and-downgrade")
  })

  it("re-applies when the active custom theme is not yet injected", () => {
    expect(
      decideCustomThemeAction({ ...base, storedThemeId: "custom:ocean", themes: [ocean], appliedCustomCss: null })
    ).toBe("register-and-reapply")
  })

  // Regression for the Codex finding: editing the active theme's CSS and hitting
  // Refresh must re-apply the new CSS even though the theme id is unchanged.
  it("re-applies when the active custom theme's CSS changed on refresh", () => {
    expect(
      decideCustomThemeAction({
        ...base,
        storedThemeId: "custom:ocean",
        themes: [{ ...ocean, rawCss: ":root{--primary:teal}" }],
        appliedCustomCss: ":root{--primary:blue}", // old, stale CSS still injected
      })
    ).toBe("register-and-reapply")
  })

  it("does not re-apply when the active custom theme's CSS is unchanged", () => {
    expect(
      decideCustomThemeAction({
        ...base,
        storedThemeId: "custom:ocean",
        themes: [ocean],
        appliedCustomCss: ocean.rawCss!,
      })
    ).toBe("register")
  })
})
