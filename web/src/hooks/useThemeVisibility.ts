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

/** The user's Show and Hide choices for the quick theme pickers. */
export function useThemeVisibility(): [ThemeVisibility, (theme: Theme, shown: boolean) => void] {
  const [visibility, setVisibility] = useClientSetting(THEME_VISIBILITY_KEY, {
    defaultValue: noChoices,
    parse: parseThemeVisibility,
  })

  const setThemeShown = useCallback((theme: Theme, shown: boolean) => {
    setVisibility((prev) => {
      // Keep only the changes: a choice equal to the default drops its entry.
      const { [theme.id]: _, ...rest } = prev
      return shown === isThemeShownByDefault(theme) ? rest : { ...rest, [theme.id]: shown }
    })
  }, [setVisibility])

  return [visibility, setThemeShown]
}
