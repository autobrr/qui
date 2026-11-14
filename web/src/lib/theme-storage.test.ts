/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import type { Theme } from "@/config/themes"
import { _setDefaultThemeVisitForTests } from "@/lib/default-theme-visit"
import {
  cacheAppliedTheme,
  cachedBackground,
  cachedThemeJson,
  storedMode,
  storedThemeId,
  storeMode,
  storeThemeId
} from "@/lib/theme-storage"

const ocean: Theme = {
  id: "custom:ocean",
  name: "Ocean",
  lightOnly: true,
  cssVars: { light: { "--background": "teal" }, dark: { "--background": "navy" } },
}

function seedStoredTheme() {
  storeThemeId("custom:ocean")
  storeMode("light")
  cacheAppliedTheme(ocean, ocean.cssVars.light)
}

function snapshot(): Record<string, string | null> {
  return Object.fromEntries(Object.keys(localStorage).sort().map(key => [key, localStorage.getItem(key)]))
}

afterEach(() => {
  _setDefaultThemeVisitForTests(false)
  localStorage.clear()
})

describe("theme storage", () => {
  it("reads back each value that it stores", () => {
    seedStoredTheme()

    expect(storedThemeId()).toBe("custom:ocean")
    expect(storedMode()).toBe("light")
    expect(JSON.parse(cachedThemeJson() ?? "null").id).toBe("custom:ocean")
    expect(cachedBackground()).toBe("teal")
    expect(localStorage.getItem("theme-light-only")).toBe("true")
  })

  it("reads no stored theme and writes nothing during a default theme visit", () => {
    seedStoredTheme()
    const before = snapshot()
    _setDefaultThemeVisitForTests(true)

    expect(storedThemeId()).toBeNull()
    expect(cachedThemeJson()).toBeNull()
    expect(cachedBackground()).toBe("")

    storeThemeId("minimal")
    storeMode("dark")
    cacheAppliedTheme({ ...ocean, id: "minimal", lightOnly: false }, { "--background": "white" })

    expect(snapshot()).toEqual(before)
    expect(storedMode()).toBe("dark")
  })

  it("reads the stored mode during a visit until a mode is set", () => {
    storeMode("light")
    _setDefaultThemeVisitForTests(true)

    expect(storedMode()).toBe("light")
  })
})
