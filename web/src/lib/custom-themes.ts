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
