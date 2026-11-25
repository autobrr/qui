/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import type { Theme } from "@/config/themes"
import { themeThumbnailPalette, themeVariationColors } from "@/lib/theme-thumbnail"

function theme(cssVars: Theme["cssVars"], options: Partial<Theme> = {}): Theme {
  return { id: "harbor", name: "Harbor", cssVars, ...options }
}

describe("themeThumbnailPalette", () => {
  it("takes the dark colors in dark mode and the light colors for a token that dark mode leaves out", () => {
    const palette = themeThumbnailPalette(theme({
      light: { "--background": "white", "--card": "ivory" },
      dark: { "--background": "black" },
    }), true, null)

    expect(palette).toEqual({ background: "black", card: "ivory" })
  })

  it("gives a light-only theme its light colors in dark mode", () => {
    const palette = themeThumbnailPalette(theme(
      { light: { "--background": "white" }, dark: { "--background": "black" } },
      { lightOnly: true }
    ), true, null)

    expect(palette.background).toBe("white")
  })

  it("follows a token that points at another token, and at the chosen variation", () => {
    const palette = themeThumbnailPalette(theme(
      {
        light: {},
        dark: {
          "--card": "navy",
          "--accent": "var(--card)",
          "--primary": "var(--variation-color)",
          "--variation-sky": "skyblue",
          "--variation-rose": "pink",
        },
      },
      { variations: ["sky", "rose"] }
    ), true, "rose")

    expect(palette).toMatchObject({ accent: "navy", primary: "pink" })
  })

  it("uses the first variation when the stored one is not a variation of the theme", () => {
    const palette = themeThumbnailPalette(theme(
      { light: { "--primary": "var(--variation-color)", "--variation-sky": "skyblue" }, dark: {} },
      { variations: ["sky"] }
    ), false, "gone")

    expect(palette.primary).toBe("skyblue")
  })

  it("reads a variation named color as a plain token", () => {
    const palette = themeThumbnailPalette(theme(
      { light: { "--primary": "var(--variation-color)", "--variation-color": "teal" }, dark: {} },
      { variations: ["color"] }
    ), false, "color")

    expect(palette.primary).toBe("teal")
  })

  it("uses the own variation color of a theme without variations, as the app does", () => {
    const palette = themeThumbnailPalette(theme(
      { light: { "--primary": "var(--variation-color)", "--variation-color": "teal" }, dark: {} }
    ), false, null)

    expect(palette.primary).toBe("teal")
  })

  it("passes a value that is not a plain reference to the browser as written", () => {
    const palette = themeThumbnailPalette(theme({
      light: { "--background": "var(--missing, rgb(20 30 40))", "--card": "color-mix(in oklab, var(--primary) 50%, white)" },
      dark: {},
    }), false, null)

    expect(palette.background).toBe("var(--missing, rgb(20 30 40))")
    expect(palette.card).toBe("color-mix(in oklab, var(--primary) 50%, white)")
  })

  it("leaves out a token in a reference cycle", () => {
    const palette = themeThumbnailPalette(theme({
      light: { "--primary": "var(--accent)", "--accent": "var(--primary)", "--background": "white" },
      dark: {},
    }), false, null)

    expect(palette).toEqual({ background: "white" })
  })
})

describe("themeVariationColors", () => {
  it("lists each variation that has a color in the mode", () => {
    const colors = themeVariationColors(theme(
      { light: { "--variation-sky": "skyblue" }, dark: { "--variation-sky": "steelblue", "--variation-rose": "pink" } },
      { variations: ["sky", "rose", "missing"] }
    ), true)

    expect(colors).toEqual([{ id: "sky", color: "steelblue" }, { id: "rose", color: "pink" }])
  })
})
