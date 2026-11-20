/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// The visit starts from the address at page load, so it is set before the modules load.
vi.hoisted(() => window.history.replaceState(null, "", "/?theme=default"))

import { registerBuiltinThemes, registerCustomThemes } from "@/config/themes"
import { isDefaultThemeVisit } from "@/lib/theme-storage"
import { getCurrentThemeMode, setTheme, setThemeMode } from "./theme"

vi.mock("./fontLoader", () => ({ loadThemeFonts: vi.fn() }))

const ocean = {
  id: "custom:ocean",
  name: "Ocean",
  isPremium: true,
  isCustom: true,
  rawCss: ":root { --background: teal; } .dark { --background: navy; }",
  cssVars: { light: { "--background": "teal" }, dark: { "--background": "navy" } },
}

const BOOT_KEYS = ["color-theme", "theme", "theme-cache", "theme-critical-vars", "theme-light-only"]

function bootCaches() {
  return BOOT_KEYS.map(key => localStorage.getItem(key))
}

function customStyle(): string | null {
  return document.getElementById("custom-theme-style")?.textContent ?? null
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  registerBuiltinThemes([{ id: "minimal", name: "Minimal", cssVars: { light: { "--background": "white" }, dark: { "--background": "black" } } }])
  registerCustomThemes([ocean])
  localStorage.setItem("color-theme", "custom:ocean")
  localStorage.setItem("theme", "light")
  localStorage.setItem("theme-cache", JSON.stringify({ id: "custom:ocean" }))
})

afterEach(() => {
  registerCustomThemes([])
  localStorage.clear()
  vi.unstubAllGlobals()
})

// The tests share one visit, which the last test ends.
describe("default theme visit", () => {
  it("paints the default theme on a system change and writes nothing", async () => {
    const before = bootCaches()

    await setTheme("custom:ocean", "dark", undefined, true)

    expect(document.documentElement.getAttribute("data-theme")).toBe("minimal")
    expect(customStyle()).toBeNull()
    expect(bootCaches()).toEqual(before)
    expect(isDefaultThemeVisit()).toBe(true)
  })

  // regression: #4124
  it("keeps the default theme on a mode change, and keeps the mode out of storage", async () => {
    const before = bootCaches()

    await setThemeMode("dark")

    expect(document.documentElement.getAttribute("data-theme")).toBe("minimal")
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    expect(getCurrentThemeMode()).toBe("dark")
    expect(bootCaches()).toEqual(before)
    expect(isDefaultThemeVisit()).toBe(true)
  })

  it("ends when the user selects a theme, which then applies and stores as normal with the visit mode", async () => {
    await setThemeMode("dark")

    await setTheme("custom:ocean")

    expect(isDefaultThemeVisit()).toBe(false)
    expect(document.documentElement.getAttribute("data-theme")).toBe("custom:ocean")
    expect(localStorage.getItem("color-theme")).toBe("custom:ocean")
    expect(localStorage.getItem("theme")).toBe("dark")
    expect(JSON.parse(localStorage.getItem("theme-cache") ?? "{}").id).toBe("custom:ocean")
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })
})
