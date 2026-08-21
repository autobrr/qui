/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { registerBuiltinThemes, getThemeById, type Theme } from "@/config/themes"
import { parseThemeCSS } from "@/utils/themeParser"
import { getCurrentTheme, setTheme } from "@/utils/theme"
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
 * Fetches the built-in theme list from the server and registers it in the
 * client theme registry. Public endpoint, so this also themes the login page.
 * Once registered, the stored selection is re-applied in case it only just
 * became resolvable (boot paints the bundled fallback until then).
 */
export function useBuiltinThemes() {
  const query = useQuery({
    queryKey: ["builtin-themes"],
    queryFn: () => api.getBuiltinThemes(),
    staleTime: Infinity,
    retry: 1,
  })

  const { data } = query

  useEffect(() => {
    if (!data) return
    registerBuiltinThemes(data.themes.map(toTheme).filter((t): t is Theme => t !== null))

    const storedId = localStorage.getItem("color-theme")
    if (storedId && getThemeById(storedId) && getCurrentTheme().id !== storedId) {
      void setTheme(storedId)
    }
  }, [data])

  return { isReady: query.isSuccess || query.isError, isError: query.isError }
}
