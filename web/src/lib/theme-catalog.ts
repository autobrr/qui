/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"

export type ThemeVisibility = Record<string, boolean>

const SHOWN_BY_DEFAULT = new Set([
  "amber-minimal",
  "amethyst-haze",
  "bubblegum",
  "catppuccin",
  "claude",
  "cyberpunk",
  "kitsune",
  "the-matrix",
  "nord",
  "perpetuity",
  "spreadsheet",
  "spreadsheet-classic",
  "synthwave",
  "tangerine",
  "tokyo-night",
])

export type ThemeGroup = "free" | "licensed" | "custom"

export const THEME_GROUPS: readonly ThemeGroup[] = ["free", "licensed", "custom"]

export function themeGroup(theme: Theme): ThemeGroup {
  if (theme.isCustom) return "custom"
  return theme.isPremium ? "licensed" : "free"
}

export function themeMatchesSearch(theme: Theme, search: string): boolean {
  const needle = search.trim().toLowerCase()
  return !needle || theme.name.toLowerCase().includes(needle) || !!theme.description?.toLowerCase().includes(needle)
}

export function isThemeShownByDefault(theme: Theme): boolean {
  return !theme.isPremium || !!theme.isCustom || SHOWN_BY_DEFAULT.has(theme.id)
}

export function isThemeShown(theme: Theme, visibility: ThemeVisibility): boolean {
  return Object.hasOwn(visibility, theme.id) ? visibility[theme.id] : isThemeShownByDefault(theme)
}

export function buildThemeCatalog(
  builtInThemes: readonly Theme[],
  customThemes: readonly Theme[],
  picker?: { visibility: ThemeVisibility; currentThemeId: string }
): Theme[] {
  const freeThemes = builtInThemes.filter((theme) => !theme.isPremium)
  const premiumThemes = builtInThemes.filter((theme) => theme.isPremium)
  const catalog = [...freeThemes, ...premiumThemes, ...customThemes]
  if (!picker) return catalog

  return catalog.filter((theme) =>
    theme.id === picker.currentThemeId || isThemeShown(theme, picker.visibility)
  )
}
