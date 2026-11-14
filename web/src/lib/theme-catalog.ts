/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"

/** Theme id to shown (true) or hidden (false). Holds only the user's changes. */
export type ThemeVisibility = Record<string, boolean>

// The licensed themes that the quick pickers showed before the community themes
// were built in (#3772). Every other licensed built-in theme starts hidden.
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

export function isThemeShownByDefault(theme: Theme): boolean {
  return !theme.isPremium || !!theme.isCustom || SHOWN_BY_DEFAULT.has(theme.id)
}

export function isThemeShown(theme: Theme, visibility: ThemeVisibility): boolean {
  return Object.hasOwn(visibility, theme.id) ? visibility[theme.id] : isThemeShownByDefault(theme)
}

/**
 * Every theme in free, premium, custom order. With picker options, only the
 * themes that the quick pickers show, plus the theme in use.
 */
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
