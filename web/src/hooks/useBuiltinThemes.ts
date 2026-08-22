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
      // Server classification is authoritative (premium dir OR CSS header);
      // the CSS header alone misses directory-classified themes.
      isPremium: entry.premium,
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
 * paint may have used a stale cached copy of the theme. A stored id that
 * resolves to a locked stub (license lapsed) paints the default via setTheme's
 * fallback, which overwrites the boot cache but leaves the stored selection
 * alone so it comes back when the license does.
 */
export function applyBuiltinThemesPayload(payload: { themes: BuiltinTheme[] }): void {
  registerBuiltinThemes(payload.themes.map(toTheme).filter((t): t is Theme => t !== null))

  // A fresh profile has no stored selection; its boot paint used the bundled
  // fallback, whose values can lag the server's default theme, so the fetched
  // default repaints it.
  const targetId = localStorage.getItem("color-theme") ?? getDefaultTheme().id
  if (getThemeById(targetId)) {
    // System change: restoring existing state must not be pushed.
    void setTheme(targetId, undefined, undefined, true)
  }
}

/**
 * Query for the built-in theme list. Public endpoint, so it also themes the
 * login page. Registration happens inside the queryFn so the registry is
 * populated before any observer re-renders on the committed data; an effect
 * would leave the first isSuccess render reading the pre-registration array.
 */
export function useBuiltinThemes() {
  const query = useQuery({
    queryKey: ["builtin-themes"],
    queryFn: async ({ signal }) => {
      const payload = await api.getBuiltinThemes(signal)
      applyBuiltinThemesPayload(payload)
      return payload
    },
    staleTime: Infinity,
    // Match the hourly license poll: a lapse or recovery server-side swaps
    // full entries and locked stubs within the hour without a reload.
    refetchInterval: 60 * 60 * 1000,
    refetchIntervalInBackground: true,
    retry: 1,
  })

  return { data: query.data, isSuccess: query.isSuccess, isError: query.isError }
}
