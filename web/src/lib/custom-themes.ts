/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseThemeCSS } from "@/utils/themeParser"
import type { Theme } from "@/config/themes"

/** Prefix applied to sideloaded custom theme ids to avoid colliding with built-ins. */
export const CUSTOM_THEME_PREFIX = "custom:"

export interface CustomThemeFile {
  id: string
  filename: string
  css: string
}

export interface CustomThemeParseError {
  filename: string
}

export interface ParsedCustomThemes {
  themes: Theme[]
  errors: CustomThemeParseError[]
}

/**
 * Parse the raw custom theme files returned by the backend into Theme objects.
 * Files that fail to parse (e.g. missing the required :root or .dark block) are
 * collected as errors instead of throwing, so the management card can surface
 * them while the valid themes still load.
 */
export function parseCustomThemes(files: CustomThemeFile[]): ParsedCustomThemes {
  const themes: Theme[] = []
  const errors: CustomThemeParseError[] = []

  for (const file of files) {
    const result = parseThemeCSS(file.css)
    if (!result) {
      errors.push({ filename: file.filename })
      continue
    }
    themes.push({
      id: `${CUSTOM_THEME_PREFIX}${file.id}`,
      name: result.metadata.name,
      description: result.metadata.description,
      lightOnly: result.metadata.lightOnly,
      isPremium: true,
      isCustom: true,
      rawCss: file.css,
      cssVars: result.cssVars,
    })
  }

  return { themes, errors }
}

/** True when the given theme id refers to a sideloaded custom theme. */
export function isCustomThemeId(themeId: string | null | undefined): boolean {
  return !!themeId && themeId.startsWith(CUSTOM_THEME_PREFIX)
}

/**
 * The side effect the custom-themes sync should perform for the current state.
 * Kept pure so the effect's branching (including the edit-and-refresh re-apply)
 * is unit-testable without mounting the hook.
 */
export type CustomThemeAction =
  | "noop" // license still resolving (loading/error) - leave everything as-is
  | "clear" // confirmed no premium, no custom selection to downgrade
  | "clear-and-downgrade" // confirmed no premium, stored custom selection -> default
  | "register" // premium: register list, active theme already matches
  | "register-and-reapply" // premium: register list and (re)apply the active custom theme
  | "register-and-downgrade" // premium: stored custom theme is gone -> default

export interface CustomThemeDecisionInput {
  isLoading: boolean
  isError: boolean
  hasPremiumAccess: boolean
  /** Whether the custom-themes query has resolved data. */
  hasData: boolean
  storedThemeId: string | null
  themes: Theme[]
  /** Raw CSS currently injected for the active custom theme, or null. */
  appliedCustomCss: string | null
}

export function decideCustomThemeAction(input: CustomThemeDecisionInput): CustomThemeAction {
  const { isLoading, isError, hasPremiumAccess, hasData, storedThemeId, themes, appliedCustomCss } = input

  // While the license check is in flight or errored, leave state untouched -
  // downgrading here would clobber a premium user's selection before it resolves.
  if (isLoading || isError) return "noop"

  if (!hasPremiumAccess) {
    return isCustomThemeId(storedThemeId) ? "clear-and-downgrade" : "clear"
  }

  // Premium, but the custom-themes list hasn't loaded yet.
  if (!hasData) return "noop"

  if (!isCustomThemeId(storedThemeId)) return "register"

  const active = themes.find(theme => theme.id === storedThemeId)
  if (!active) return "register-and-downgrade"

  // Re-apply when the active theme isn't currently injected, or its CSS changed
  // (the file was edited and the user hit Refresh). Comparing content - not the
  // id - is what makes the edit-and-refresh workflow update the live theme.
  return appliedCustomCss !== active.rawCss ? "register-and-reapply" : "register"
}
