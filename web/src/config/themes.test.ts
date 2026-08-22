/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { parseCachedTheme } from "./themes"

afterEach(() => {
  localStorage.clear()
  vi.resetModules()
})

describe("parseCachedTheme", () => {
  it("accepts a cached theme with full css vars", () => {
    const theme = parseCachedTheme(JSON.stringify({
      id: "swizzin",
      name: "Swizzin",
      cssVars: { light: { "--primary": "green" }, dark: { "--primary": "darkgreen" } },
    }))
    expect(theme?.id).toBe("swizzin")
  })

  it("rejects malformed, incomplete, and fallback-id caches", () => {
    expect(parseCachedTheme(null)).toBeNull()
    expect(parseCachedTheme("{not json")).toBeNull()
    expect(parseCachedTheme(JSON.stringify({ id: "x" }))).toBeNull()
    expect(parseCachedTheme(JSON.stringify({ id: "x", cssVars: { light: {} } }))).toBeNull()
    // The bundled fallback never comes from the cache.
    expect(parseCachedTheme(JSON.stringify({
      id: "minimal",
      cssVars: { light: {}, dark: {} },
    }))).toBeNull()
  })
})

describe("registry boot hydration", () => {
  it("resolves the cached theme at module init, before the API answers", async () => {
    localStorage.setItem("theme-cache", JSON.stringify({
      id: "swizzin",
      name: "Swizzin",
      cssVars: { light: { "--primary": "green" }, dark: { "--primary": "darkgreen" } },
    }))
    vi.resetModules()

    const fresh = await import("./themes")
    expect(fresh.getThemeById("swizzin")?.name).toBe("Swizzin")
    expect(fresh.getThemeById("minimal")).toBeTruthy()
  })
})
