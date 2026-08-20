/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"

export function buildThemeCatalog(
  builtInThemes: readonly Theme[],
  customThemes: readonly Theme[]
): Theme[] {
  const freeThemes = builtInThemes.filter((theme) => !theme.isPremium)
  const premiumThemes = builtInThemes.filter((theme) => theme.isPremium)

  return [...freeThemes, ...premiumThemes, ...customThemes]
}
