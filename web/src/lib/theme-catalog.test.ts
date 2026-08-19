/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { buildThemeCatalog } from "@/lib/theme-catalog"
import { describe, expect, it } from "vitest"

function makeTheme(
  id: string,
  options: Pick<Theme, "isPremium" | "isCustom"> = {}
): Theme {
  return {
    id,
    name: id,
    ...options,
    cssVars: {
      light: {},
      dark: {},
    },
  }
}

describe("buildThemeCatalog", () => {
  it("keeps every theme in free, premium, custom order", () => {
    const builtInThemes = [
      makeTheme("premium-one", { isPremium: true }),
      makeTheme("free-one"),
      makeTheme("premium-two", { isPremium: true }),
      makeTheme("free-two"),
    ]
    const customThemes = [
      makeTheme("custom-one", { isCustom: true }),
      makeTheme("custom-two", { isCustom: true }),
    ]

    const catalog = buildThemeCatalog(builtInThemes, customThemes)

    expect(catalog.map((theme) => theme.id)).toEqual([
      "free-one",
      "free-two",
      "premium-one",
      "premium-two",
      "custom-one",
      "custom-two",
    ])
  })

  it("does not mutate either source list", () => {
    const builtInThemes = [
      makeTheme("premium", { isPremium: true }),
      makeTheme("free"),
    ]
    const customThemes = [makeTheme("custom", { isCustom: true })]

    buildThemeCatalog(builtInThemes, customThemes)

    expect(builtInThemes.map((theme) => theme.id)).toEqual(["premium", "free"])
    expect(customThemes.map((theme) => theme.id)).toEqual(["custom"])
  })
})
