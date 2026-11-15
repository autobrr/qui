/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { endCustomThemePreview, previewCustomTheme } from "@/lib/custom-theme-preview"
import { getAppliedCustomThemeCss, setTheme, setThemeMode } from "./theme"
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

afterEach(async () => {
  await endCustomThemePreview()
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

const customTheme = {
  id: "custom:ocean",
  name: "Ocean",
  isPremium: true,
  isCustom: true,
  rawCss: ":root { --background: teal; } .dark { --background: navy; }",
  cssVars: { light: { "--background": "teal" }, dark: { "--background": "navy" } },
}

function bootCaches() {
  return ["color-theme", "theme", "theme-cache", "theme-critical-vars", "theme-light-only"].map(key => localStorage.getItem(key))
}

function customStyle(): string | null {
  return document.getElementById("custom-theme-style")?.textContent ?? null
}

describe("custom theme preview", () => {
  it("shows the css without a write to the stored theme or the boot caches", async () => {
    await setTheme("minimal")
    const before = bootCaches()
    const events: unknown[] = []
    const listener = (event: Event) => events.push(event)
    window.addEventListener("themechange", listener)

    previewCustomTheme(":root { --background: red; } .dark { --background: maroon; }")

    window.removeEventListener("themechange", listener)
    expect(customStyle()).toContain("--background: red")
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("")
    expect(document.documentElement.style.backgroundColor).toBe("")
    expect(bootCaches()).toEqual(before)
    expect(events).toEqual([])
  })

  it("ends by painting the active theme again", async () => {
    registerCustomThemes([customTheme])
    await setTheme("custom:ocean")
    previewCustomTheme(":root { --background: red; } .dark { --background: maroon; }")

    await endCustomThemePreview()

    expect(customStyle()).toBe(customTheme.rawCss)
    expect(document.documentElement.getAttribute("data-theme")).toBe("custom:ocean")
  })

  it("ends on a built-in theme without the preview style", async () => {
    await setTheme("minimal")
    previewCustomTheme(":root { --background: red; } .dark { --background: maroon; }")

    await endCustomThemePreview()

    expect(customStyle()).toBeNull()
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("red")
  })

  // regression: #4124
  it("stays on the page through a mode change and a theme change", async () => {
    const draft = ":root { --background: red; } .dark { --background: maroon; }"
    registerCustomThemes([customTheme])
    await setTheme("minimal", "light")
    previewCustomTheme(draft)

    await setThemeMode("dark")
    expect(customStyle()).toBe(draft)
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("")

    await setTheme("custom:ocean")
    expect(customStyle()).toBe(draft)

    await endCustomThemePreview()
    expect(customStyle()).toBe(customTheme.rawCss)
  })

  // regression: #4124
  it("reports the css of the applied theme, not the draft", async () => {
    registerCustomThemes([customTheme])
    await setTheme("custom:ocean")

    previewCustomTheme(":root { --background: red; } .dark { --background: maroon; }")

    expect(getAppliedCustomThemeCss()).toBe(customTheme.rawCss)
  })

  it("leaves the page as it is for css that does not parse as a theme", async () => {
    await setTheme("minimal")

    expect(previewCustomTheme("body { color: red; }")).toEqual({ kind: "missing-root" })

    expect(customStyle()).toBeNull()
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("red")
  })

  // regression: #4124
  it("keeps css that loads an outside file off the page, and keeps the last good preview", async () => {
    const good = ":root { --background: red; } .dark { --background: maroon; }"
    await setTheme("minimal")
    expect(previewCustomTheme(good)).toBeNull()

    const outside = `${good}
body { background: url(https://example.invalid/a.png); }`
    expect(previewCustomTheme(outside)).toEqual({ kind: "outside-file", line: 2 })

    expect(customStyle()).toBe(good)
  })
})

describe("custom theme preview mode", () => {
  // regression: #4124
  it("shows a light-only theme in light mode and an ordinary theme in the selected mode", async () => {
    await setTheme("minimal", "dark")
    expect(document.documentElement.classList.contains("dark")).toBe(true)

    previewCustomTheme("/* @lightOnly: true */ :root { --background: red; } .dark { --background: maroon; }")
    expect(document.documentElement.classList.contains("dark")).toBe(false)

    previewCustomTheme(":root { --background: red; } .dark { --background: maroon; }")
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })
})
