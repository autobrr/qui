/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { afterEach, describe, expect, it } from "vitest"
import { _resetClientSettingsForTests } from "@/lib/client-settings"
import {
  applyGlassOpacity,
  GLASS_DEFAULT_PERCENT,
  GLASS_SOLID_PERCENT,
  glassIsOn,
  glassSurface,
  migrateLegacyGlassTheme,
  pageBackground,
  parseGlassPercent
} from "@/lib/glass"

afterEach(() => {
  _resetClientSettingsForTests()
  localStorage.clear()
  document.getElementById("qui-glass-blur")?.remove()
  delete document.documentElement.dataset.glass
  document.documentElement.style.backgroundColor = ""
  document.documentElement.style.removeProperty("--background")
  document.body.style.backgroundColor = ""
  delete window.quiDesktop
})

describe("glass frost (BR-desktop-05)", () => {
  it("keeps the chosen percent of the theme color, and 100% is solid", () => {
    expect(GLASS_DEFAULT_PERCENT).toBe(60)
    expect(GLASS_SOLID_PERCENT).toBe(100)
    expect(glassIsOn(60)).toBe(true)
    expect(glassIsOn(100)).toBe(false)
    expect(parseGlassPercent("true")).toBe(60)
    expect(parseGlassPercent("false")).toBe(100)
    expect(parseGlassPercent(null)).toBe(100)
    expect(parseGlassPercent("45")).toBe(45)
    expect(glassSurface("oklch(0.145 0 0)", 60)).toBe(
      "color-mix(in oklab, oklch(0.145 0 0) 60%, transparent)"
    )
    expect(pageBackground("#252525", 60)).toEqual({
      html: glassSurface("#252525", 60),
      body: "transparent",
    })
    expect(pageBackground("#252525", 100)).toEqual({
      html: "#252525",
      body: "#252525",
    })
  })

  it("paints the document shell, not only a dialog, when glass is on", () => {
    document.documentElement.style.setProperty("--background", "oklch(0.145 0 0)")
    const solid = document.documentElement.style.getPropertyValue("--background").trim()
    const calls: boolean[] = []
    window.quiDesktop = {
      setWindowMaterial: async (enabled: boolean) => {
        calls.push(enabled)
      },
    }

    applyGlassOpacity(60)

    expect(document.documentElement.dataset.glass).toBe("on")
    expect(document.getElementById("qui-glass-blur")?.textContent).toContain("backdrop-filter:blur(24px)")
    expect(document.documentElement.style.getPropertyValue("--glass-surface")).toBe("60%")
    expect(document.documentElement.style.backgroundColor).toBe(glassSurface(solid, 60))
    expect(document.body.style.backgroundColor).toBe("transparent")
    expect(calls).toEqual([true])

    applyGlassOpacity(100)

    expect(document.documentElement.dataset.glass).toBeUndefined()
    expect(document.getElementById("qui-glass-blur")).toBeNull()
    expect(document.documentElement.style.backgroundColor).toBe(solid)
    expect(document.body.style.backgroundColor).toBe(solid)
    expect(calls).toEqual([true, false])
  })

  it("turns a saved glassmorphism theme into the switch and Minimal", () => {
    let stored = "glassmorphism"
    migrateLegacyGlassTheme("glassmorphism", (id) => {
      stored = id
    })
    expect(stored).toBe("minimal")
    expect(localStorage.getItem("qui-glassmorphism")).toBe("60")
  })

  it("frosts the page surfaces in CSS, including the shell and the sidebar", () => {
    const css = readFileSync(
      path.join(path.dirname(fileURLToPath(import.meta.url)), "../index.css"),
      "utf8"
    )
    const rule = css.slice(css.indexOf("html[data-glass=\"on\"]"))
    expect(rule).toContain("var(--glass-surface)")
    expect(rule).toContain("backdrop-filter: blur(24px)")
    expect(rule).toContain(".bg-background")
    expect(rule).toContain(".bg-sidebar")
    expect(rule).toContain(".bg-card")
    expect(rule).toContain(".bg-popover")
    expect(rule).not.toContain("[data-theme=\"glassmorphism\"]")
  })
})
