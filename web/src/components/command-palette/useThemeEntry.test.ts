/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { type PaletteEntry, searchEntries } from "./palette"
import { useThemeEntry } from "./useThemeEntry"

const fixture = vi.hoisted(() => {
  const theme = (id: string, name: string, extra: Partial<Theme> = {}): Theme => ({ id, name, cssVars: { light: { "--background": `${id}-light` }, dark: { "--background": `${id}-dark` } }, ...extra })
  const builtIn = [theme("minimal", "Minimal"), theme("bubblegum", "Bubblegum", { isPremium: true }), theme("kitsune", "Kitsune", { isPremium: true, locked: true })]
  return {
    builtIn,
    custom: { customThemes: [theme("custom:mine", "Mine", { isCustom: true })] },
    themeHook: { mode: "auto", dark: false, currentTheme: builtIn[1], setTheme: () => Promise.resolve(), setThemeMode: () => Promise.resolve() },
    translation: { t: (key: string) => key },
  }
})

vi.mock("react-i18next", () => ({ useTranslation: () => fixture.translation }))
vi.mock("@/config/themes", () => ({ themes: fixture.builtIn }))
vi.mock("@/hooks/useBuiltinThemes", () => ({ useBuiltinThemes: () => undefined }))
vi.mock("@/hooks/useCustomThemes", () => ({ useCustomThemes: () => fixture.custom }))
vi.mock("@/hooks/useTheme", () => ({ useTheme: () => fixture.themeHook }))
vi.mock("@/utils/theme", () => ({ getThemeVariation: () => null }))

function themeEntry() {
  return renderHook(() => useThemeEntry(() => {})).result.current
}

const ids = (entries: PaletteEntry[]) => entries.map(entry => entry.id)

afterEach(() => {
  cleanup()
  fixture.themeHook.dark = false
})

describe("useThemeEntry", () => {
  it("lists the modes, then every theme", () => {
    const [modes, themes] = themeEntry().sections ?? []

    expect(ids(modes.entries)).toEqual(["theme-mode.light", "theme-mode.dark", "theme-mode.auto"])
    expect(ids(themes.entries)).toEqual(["theme.minimal", "theme.bubblegum", "theme.kitsune", "theme.custom:mine"])
  })

  it("disables a theme that needs a license, and gives it no action", () => {
    const entries = (themeEntry().sections ?? []).flatMap(group => group.entries)

    expect(ids(entries.filter(entry => entry.disabled))).toEqual(["theme.kitsune"])
    expect(ids(entries.filter(entry => !entry.run))).toEqual(["theme.kitsune"])
  })

  it("gives each theme its colors in the mode that shows", () => {
    const backgrounds = () => (themeEntry().sections ?? [])[1].entries.map(entry => entry.thumbnail?.background)

    expect(backgrounds()).toEqual(["minimal-light", "bubblegum-light", "kitsune-light", "custom:mine-light"])
    fixture.themeHook.dark = true
    expect(backgrounds()).toEqual(["minimal-dark", "bubblegum-dark", "kitsune-dark", "custom:mine-dark"])
  })

  it("marks the mode and the theme in use", () => {
    const entries = (themeEntry().sections ?? []).flatMap(group => group.entries)

    expect(ids(entries.filter(entry => entry.current))).toEqual(["theme-mode.auto", "theme.bubblegum"])
  })

  it("lets a search from the first level find a mode, and only a search of its own list find a theme", () => {
    const entry = themeEntry()
    const firstLevel = [{ id: "actions", entries: [entry] }]

    expect(ids(searchEntries(firstLevel, "dark"))).toEqual(["theme-mode.dark"])
    expect(searchEntries(firstLevel, "bubble")).toEqual([])
    expect(ids(searchEntries(entry.sections ?? [], "bubble"))).toEqual(["theme.bubblegum"])
  })
})
