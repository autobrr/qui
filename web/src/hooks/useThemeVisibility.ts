/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import type { Theme } from "@/config/themes"
import { useClientSetting } from "@/lib/client-settings"
import { isThemeShownByDefault, type ThemeVisibility } from "@/lib/theme-catalog"

const THEME_VISIBILITY_KEY = "qui-theme-visibility"

const noChoices: ThemeVisibility = {}

function parseThemeVisibility(raw: string): ThemeVisibility {
  const parsed: unknown = JSON.parse(raw)
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("not an object")
  }
  return Object.fromEntries(Object.entries(parsed).filter(([, shown]) => typeof shown === "boolean"))
}

/**
 * The user's Show and Hide choices for the quick theme pickers, a setter that
 * shows or hides a list of themes in one write, and a setter that replaces all
 * choices at once ({} restores the defaults).
 */
export function useThemeVisibility(): [
  ThemeVisibility,
  (themes: readonly Theme[], shown: boolean) => void,
  (next: ThemeVisibility) => void
] {
  const [visibility, setVisibility] = useClientSetting(THEME_VISIBILITY_KEY, {
    defaultValue: noChoices,
    parse: parseThemeVisibility,
  })

  const setThemesShown = useCallback((themes: readonly Theme[], shown: boolean) => {
    setVisibility((prev) => {
      const next = { ...prev }
      for (const theme of themes) {
        // Keep only the changes: a choice equal to the default drops its entry.
        if (shown === isThemeShownByDefault(theme)) delete next[theme.id]
        else next[theme.id] = shown
      }
      return next
    })
  }, [setVisibility])

  return [visibility, setThemesShown, setVisibility]
}
