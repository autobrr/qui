/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { setTheme, setThemeMode } from "./theme"
import { parseCachedTheme, registerBuiltinThemes, registerCustomThemes } from "@/config/themes"
import { getStoredVariation } from "@/hooks/usePersistedThemeVariation"

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
  registerCustomThemes([])
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

  it("dispatches system-driven applications as system changes", async () => {
    const events: boolean[] = []
    const listener = (event: Event) => {
      events.push((event as CustomEvent).detail.isSystemChange)
    }
    window.addEventListener("themechange", listener)
    await setTheme("minimal", undefined, undefined, true)
    window.removeEventListener("themechange", listener)

    // Hydration and server pulls restore existing state; the sync hook must
    // never push them.
    expect(events).toEqual([true])
  })

  it("keeps a requested variation while the selected theme is locked", async () => {
    await setTheme("locked-premium", "dark", "purple", true)

    expect(getStoredVariation("locked-premium")).toBe("purple")
  })

  it("keeps the stored selection through a mode toggle on the fallback", async () => {
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
    // A user change dispatched with the applied fallback theme; the sync hook
    // maps it back to the stored selection before pushing to the server.
    expect(events).toEqual([{ themeId: "minimal", isSystemChange: false }])
  })
})

describe("boot cache for custom themes", () => {
  it("caches an applied custom theme with its raw CSS so a refresh can repaint it", async () => {
    registerCustomThemes([{
      id: "custom:ocean",
      name: "Ocean",
      isCustom: true,
      rawCss: ":root { --background: navy; }",
      cssVars,
    }])

    await setTheme("custom:ocean")

    const cached = parseCachedTheme(localStorage.getItem("theme-cache"))
    expect(cached?.id).toBe("custom:ocean")
    expect(cached?.isCustom).toBe(true)
    expect(cached?.rawCss).toBe(":root { --background: navy; }")
  })
})
