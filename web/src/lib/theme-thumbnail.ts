/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"

const THUMBNAIL_TOKENS = {
  background: "--background",
  foreground: "--foreground",
  mutedForeground: "--muted-foreground",
  card: "--card",
  border: "--border",
  sidebar: "--sidebar",
  primary: "--primary",
  secondary: "--secondary",
  accent: "--accent",
} as const

export type ThemeThumbnailPalette = Partial<Record<keyof typeof THUMBNAIL_TOKENS, string>>

// var(--name) or var(--name, fallback), with no parentheses in the fallback.
const VAR_REFERENCE = /var\(\s*(--[\w-]+)\s*(?:,\s*([^()]*?))?\s*\)/g

/**
 * The colors that a thumbnail of the theme paints in one mode. A light-only
 * theme always gives its light colors. A token that the theme does not set
 * is missing: a locked theme sends only primary, secondary, and accent.
 */
export function themeThumbnailPalette(theme: Theme, dark: boolean, variation: string | null): ThemeThumbnailPalette {
  const own = dark && !theme.lightOnly ? theme.cssVars.dark : theme.cssVars.light
  const root = theme.cssVars.light
  const token = (name: string): string | undefined => own[name] ?? root[name]
  const lookup = (name: string): string | undefined => {
    if (name === "--variation-color") {
      const id = variation && theme.variations?.includes(variation) ? variation : theme.variations?.[0]
      // Like applyTheme, a missing variation color keeps the own --variation-color of the theme.
      return (id ? token(`--variation-${id}`) : undefined) ?? token("--variation-color")
    }
    return token(name)
  }
  // Each reference resolves against this theme, also inside a color function, because the page
  // would resolve it against the theme in use. Ten passes end a cycle.
  const resolve = (name: string): string | undefined => {
    let value = lookup(name)
    for (let step = 0; value !== undefined && step < 10; step++) {
      if (!value.includes("var(")) return value
      let missing = false
      value = value.replace(VAR_REFERENCE, (_match, reference: string, fallback?: string) => {
        const found = lookup(reference) ?? fallback
        if (found === undefined) missing = true
        return found ?? ""
      })
      if (missing) return undefined
    }
    return undefined
  }

  const palette: ThemeThumbnailPalette = {}
  for (const [key, token] of Object.entries(THUMBNAIL_TOKENS) as Array<[keyof typeof THUMBNAIL_TOKENS, string]>) {
    const value = resolve(token)
    if (value) palette[key] = value
  }
  return palette
}

/** The color variations of a theme, each with its color in one mode. */
export function themeVariationColors(theme: Theme, dark: boolean): Array<{ id: string; color: string }> {
  const own = dark && !theme.lightOnly ? theme.cssVars.dark : theme.cssVars.light
  return (theme.variations ?? []).flatMap((id) => {
    const color = own[`--variation-${id}`] ?? theme.cssVars.light[`--variation-${id}`]
    return color ? [{ id, color }] : []
  })
}
