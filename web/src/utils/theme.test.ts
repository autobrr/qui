/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { setTheme, setThemeMode } from "./theme"
import { registerBuiltinThemes } from "@/config/themes"

vi.mock("./fontLoader", () => ({ loadThemeFonts: vi.fn() }))

const cssVars = {
  light: { "--background": "white", "--primary": "red" },
  dark: { "--background": "black", "--primary": "darkred" },
}

beforeEach(() => {
  // jsdom has no matchMedia; setTheme resolves the system preference with it.
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })))
  registerBuiltinThemes([
    { id: "minimal", name: "Minimal", cssVars },
    { id: "locked-premium", name: "Locked", isPremium: true, locked: true, cssVars: { light: {}, dark: {} } },
  ])
})

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe("setTheme locked fallback", () => {
  it("applies the default without persisting or syncing the downgrade", async () => {
    localStorage.setItem("color-theme", "locked-premium")

    const events: Array<{ themeId: string; isSystemChange: boolean }> = []
    const listener = (event: Event) => {
      const { theme, isSystemChange } = (event as CustomEvent).detail
      events.push({ themeId: theme.id, isSystemChange })
    }
    window.addEventListener("themechange", listener)
    await setTheme("locked-premium")
    window.removeEventListener("themechange", listener)

    expect(document.documentElement.getAttribute("data-theme")).toBe("minimal")
    // The stored selection survives so it comes back when the license does.
    expect(localStorage.getItem("color-theme")).toBe("locked-premium")
    // Flagged as system-driven so useThemeSettingsSync never pushes the
    // downgrade to the server, which would overwrite the stored selection.
    expect(events).toEqual([{ themeId: "minimal", isSystemChange: true }])
  })

  it("keeps a mode toggle from syncing the fallback over the stored selection", async () => {
    localStorage.setItem("color-theme", "locked-premium")

    const events: Array<{ themeId: string; isSystemChange: boolean }> = []
    const listener = (event: Event) => {
      const { theme, isSystemChange } = (event as CustomEvent).detail
      events.push({ themeId: theme.id, isSystemChange })
    }
    window.addEventListener("themechange", listener)
    await setThemeMode("dark")
    window.removeEventListener("themechange", listener)

    expect(localStorage.getItem("color-theme")).toBe("locked-premium")
    // The applied theme is the fallback default, not a user selection: flagged
    // system-driven so the sync never PUTs it over the stored premium id.
    expect(events).toEqual([{ themeId: "minimal", isSystemChange: true }])
  })
})
