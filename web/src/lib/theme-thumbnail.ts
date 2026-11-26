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

const VAR_REFERENCE = /^var\(\s*(--[\w-]+)\s*\)$/

export function themeThumbnailPalette(theme: Theme, dark: boolean, variation: string | null): ThemeThumbnailPalette {
  const own = dark && !theme.lightOnly ? theme.cssVars.dark : theme.cssVars.light
  const root = theme.cssVars.light
  const token = (name: string): string | undefined => own[name] ?? root[name]
  const lookup = (name: string): string | undefined => {
    if (name === "--variation-color") {
      const id = variation && theme.variations?.includes(variation) ? variation : theme.variations?.[0]
      return (id ? token(`--variation-${id}`) : undefined) ?? token("--variation-color")
    }
    return token(name)
  }
  const resolve = (name: string): string | undefined => {
    let value = lookup(name)
    for (let step = 0; value !== undefined && step < 10; step++) {
      const reference = VAR_REFERENCE.exec(value)
      if (!reference) return value
      value = lookup(reference[1])
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

export function themeVariationColors(theme: Theme, dark: boolean): Array<{ id: string; color: string }> {
  const own = dark && !theme.lightOnly ? theme.cssVars.dark : theme.cssVars.light
  return (theme.variations ?? []).flatMap((id) => {
    const color = own[`--variation-${id}`] ?? theme.cssVars.light[`--variation-${id}`]
    return color ? [{ id, color }] : []
  })
}
