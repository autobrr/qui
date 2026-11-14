/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"

// The visit starts from the address at page load, so it is set before the modules load.
vi.hoisted(() => window.history.replaceState(null, "", "/?theme=default"))

import type { Theme } from "@/config/themes"
import {
  cacheAppliedTheme,
  cachedBackground,
  cachedThemeJson,
  endDefaultThemeVisit,
  isDefaultThemeVisit,
  storedMode,
  storedThemeId,
  storeMode,
  storeThemeId
} from "@/lib/theme-storage"

const minimal: Theme = {
  id: "minimal",
  name: "Minimal",
  cssVars: { light: { "--background": "white" }, dark: { "--background": "black" } },
}

function seedStoredTheme() {
  localStorage.setItem("color-theme", "custom:ocean")
  localStorage.setItem("theme", "light")
  localStorage.setItem("theme-cache", JSON.stringify({ id: "custom:ocean" }))
  localStorage.setItem("theme-critical-vars", JSON.stringify({ background: "teal", foreground: "white" }))
  localStorage.setItem("theme-light-only", "true")
}

function snapshot(): Record<string, string | null> {
  return Object.fromEntries(Object.keys(localStorage).sort().map(key => [key, localStorage.getItem(key)]))
}

afterEach(() => {
  localStorage.clear()
})

// The tests share one visit, which the last test ends.
describe("theme storage during a default theme visit", () => {
  it("reads the stored mode until a mode is set", () => {
    seedStoredTheme()

    expect(isDefaultThemeVisit()).toBe(true)
    expect(storedMode()).toBe("light")
  })

  it("reads no stored theme and writes nothing", () => {
    seedStoredTheme()
    const before = snapshot()

    expect(storedThemeId()).toBeNull()
    expect(cachedThemeJson()).toBeNull()
    expect(cachedBackground()).toBe("")

    storeThemeId("minimal")
    storeMode("dark")
    cacheAppliedTheme(minimal, minimal.cssVars.dark)

    expect(snapshot()).toEqual(before)
    expect(storedMode()).toBe("dark")
  })

  it("reads and writes the stored theme again after the visit ends", () => {
    seedStoredTheme()

    endDefaultThemeVisit()

    expect(isDefaultThemeVisit()).toBe(false)
    expect(storedThemeId()).toBe("custom:ocean")
    expect(storedMode()).toBe("light")
    storeThemeId("minimal")
    expect(localStorage.getItem("color-theme")).toBe("minimal")
  })
})
