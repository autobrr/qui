/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseThemeCSS } from "@/utils/themeParser"
import type { Theme } from "@/config/themes"

export const CUSTOM_THEME_PREFIX = "custom:"

export interface CustomThemeFile {
  id: string
  filename: string
  css: string
}

export interface CustomThemeParseError {
  id: string
  filename: string
  css: string
}

export interface ParsedCustomThemes {
  themes: Theme[]
  errors: CustomThemeParseError[]
}

export function parseCustomThemes(files: CustomThemeFile[]): ParsedCustomThemes {
  const themes: Theme[] = []
  const errors: CustomThemeParseError[] = []

  for (const file of files) {
    const result = parseThemeCSS(file.css)
    if (!result) {
      errors.push({ id: file.id, filename: file.filename, css: file.css })
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

export function isCustomThemeId(themeId: string | null | undefined): boolean {
  return !!themeId && themeId.startsWith(CUSTOM_THEME_PREFIX)
}

export type CustomThemeAction =
  | "noop"
  | "clear"
  | "clear-and-downgrade"
  | "register"
  | "register-and-reapply"
  | "register-and-downgrade"

export interface CustomThemeDecisionInput {
  isLoading: boolean
  isError: boolean
  hasPremiumAccess: boolean
  hasData: boolean
  storedThemeId: string | null
  themes: Theme[]
  appliedCustomCss: string | null
}

export function decideCustomThemeAction(input: CustomThemeDecisionInput): CustomThemeAction {
  const { isLoading, isError, hasPremiumAccess, hasData, storedThemeId, themes, appliedCustomCss } = input

  if (isLoading || isError) return "noop"

  if (!hasPremiumAccess) {
    return isCustomThemeId(storedThemeId) ? "clear-and-downgrade" : "clear"
  }

  if (!hasData) return "noop"

  if (!isCustomThemeId(storedThemeId)) return "register"

  const active = themes.find(theme => theme.id === storedThemeId)
  if (!active) return "register-and-downgrade"

  return appliedCustomCss !== active.rawCss ? "register-and-reapply" : "register"
}
