/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import type { Theme } from "@/config/themes"
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

afterEach(() => {
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
})
