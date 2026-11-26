/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { registerBuiltinThemes, getThemeById, getDefaultTheme, type Theme } from "@/config/themes"
import { parseThemeCSS } from "@/utils/themeParser"
import { storedThemeId } from "@/lib/theme-storage"
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
      isPremium: entry.premium,
      lightOnly: parsed.metadata.lightOnly,
      variations: parsed.variations,
      cssVars: parsed.cssVars,
    }
  }

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

export function applyBuiltinThemesPayload(payload: { themes: BuiltinTheme[] }): void {
  registerBuiltinThemes(payload.themes.map(toTheme).filter((t): t is Theme => t !== null))

  const targetId = storedThemeId() ?? getDefaultTheme().id
  if (getThemeById(targetId)) {
    void setTheme(targetId, undefined, undefined, true)
  }
}

export function useBuiltinThemes() {
  const query = useQuery({
    queryKey: ["builtin-themes"],
    queryFn: async ({ signal }) => {
      const payload = await api.getBuiltinThemes(signal)
      applyBuiltinThemesPayload(payload)
      return payload
    },
    staleTime: Infinity,
    refetchInterval: 60 * 60 * 1000,
    refetchIntervalInBackground: true,
    retry: 1,
  })

  return { data: query.data, isSuccess: query.isSuccess, isError: query.isError }
}
