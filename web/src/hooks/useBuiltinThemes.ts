/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { registerBuiltinThemes, getThemeById, getDefaultTheme, type Theme } from "@/config/themes"
import { parseThemeCSS } from "@/utils/themeParser"
import { setTheme } from "@/utils/theme"
import type { BuiltinTheme } from "@/types"

function toTheme(entry: BuiltinTheme): Theme | null {
  if (entry.css) {
    const parsed = parseThemeCSS(entry.css)
    if (!parsed) {
      console.warn(`Failed to parse built-in theme: ${entry.id}`)
      return null
    }
    return {
      id: entry.id,
      name: parsed.metadata.name,
      description: parsed.metadata.description,
      isPremium: parsed.metadata.isPremium,
      lightOnly: parsed.metadata.lightOnly,
      variations: parsed.variations,
      cssVars: parsed.cssVars,
    }
  }

  // Locked premium theme: no CSS, only the preview swatch colors. It renders
  // in the picker but can never be applied (the premium gate blocks it).
  return {
    id: entry.id,
    name: entry.name,
    description: entry.description,
    isPremium: true,
    locked: true,
    cssVars: {
      light: entry.preview?.light ?? {},
      dark: entry.preview?.dark ?? {},
    },
  }
}

/**
 * Registers a fetched theme payload in the client registry and re-applies the
 * stored selection: it may only just have become resolvable, and the boot
 * paint may have used a stale cached copy of the theme. A stored theme that
 * resolves to a locked stub (license lapsed) is downgraded to the default,
 * which also overwrites the boot cache. Called once per payload by
 * BuiltinThemesLoader.
 */
export function applyBuiltinThemesPayload(payload: { themes: BuiltinTheme[] }): void {
  registerBuiltinThemes(payload.themes.map(toTheme).filter((t): t is Theme => t !== null))

  const storedId = localStorage.getItem("color-theme")
  const stored = storedId ? getThemeById(storedId) : undefined
  if (stored && !stored.locked) {
    void setTheme(stored.id)
  } else if (stored?.locked) {
    void setTheme(getDefaultTheme().id)
  }
}

/**
 * Query for the built-in theme list. Public endpoint, so it also themes the
 * login page. Registration happens once, in BuiltinThemesLoader; every other
 * caller subscribes purely for the re-render when the registry lands.
 */
export function useBuiltinThemes() {
  const query = useQuery({
    queryKey: ["builtin-themes"],
    queryFn: () => api.getBuiltinThemes(),
    staleTime: Infinity,
    retry: 1,
  })

  return { data: query.data, isSuccess: query.isSuccess, isError: query.isError }
}
