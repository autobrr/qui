/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useMemo } from "react"
import { useQuery, type QueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import { parseCustomThemes, decideCustomThemeAction, type ParsedCustomThemes } from "@/lib/custom-themes"
import { registerCustomThemes, getDefaultTheme } from "@/config/themes"
import { setTheme, getAppliedCustomThemeCss } from "@/utils/theme"
import { isDefaultThemeVisit } from "@/lib/default-theme-visit"
import type { CustomThemeFile } from "@/lib/custom-themes"

const EMPTY: ParsedCustomThemes = { themes: [], errors: [] }
const CUSTOM_THEMES_KEY = ["custom-themes"]

/**
 * Loads the custom themes again and registers them at once, so a theme saved a
 * moment ago resolves before the next render.
 */
export async function refreshCustomThemes(queryClient: QueryClient): Promise<CustomThemeFile[]> {
  // A list request that started before a write can miss it, so it does not answer this one.
  await queryClient.cancelQueries({ queryKey: CUSTOM_THEMES_KEY })
  const data = await queryClient.fetchQuery({ queryKey: CUSTOM_THEMES_KEY, queryFn: () => api.getCustomThemes(), staleTime: 0 })
  registerCustomThemes(parseCustomThemes(data.themes).themes)
  return data.themes
}

/**
 * Loads the custom themes (premium-gated). It fetches the directory
 * listing only when the user has premium access, parses each file with the
 * shared theme parser, registers the results so they resolve in the picker,
 * and re-applies / downgrades the stored selection as the registry changes.
 */
export function useCustomThemes() {
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess()

  const query = useQuery({
    queryKey: CUSTOM_THEMES_KEY,
    queryFn: () => api.getCustomThemes(),
    enabled: hasPremiumAccess,
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  })

  const parsed = useMemo<ParsedCustomThemes>(() => {
    if (!query.data) return EMPTY
    return parseCustomThemes(query.data.themes)
  }, [query.data])

  // Sync the runtime registry and the stored selection with the latest list.
  useEffect(() => {
    const storedThemeId = localStorage.getItem("color-theme")
    const action = decideCustomThemeAction({
      isLoading,
      isError,
      hasPremiumAccess,
      hasData: !!query.data,
      storedThemeId,
      themes: parsed.themes,
      appliedCustomCss: getAppliedCustomThemeCss(),
      defaultThemeVisit: isDefaultThemeVisit(),
    })

    switch (action) {
      case "clear":
        registerCustomThemes([])
        break
      case "clear-and-downgrade":
        registerCustomThemes([])
        void setTheme(getDefaultTheme().id)
        break
      case "register":
        registerCustomThemes(parsed.themes)
        break
      case "register-and-reapply":
        // Apply the stored custom theme: it wasn't resolvable during the initial
        // pre-fetch init, or its file was edited and refreshed (CSS changed).
        registerCustomThemes(parsed.themes)
        // System change: restoring existing state must not be pushed.
        void setTheme(storedThemeId!, undefined, undefined, true)
        break
      case "register-and-downgrade":
        // The selected custom theme's file was removed/renamed - fall back.
        registerCustomThemes(parsed.themes)
        void setTheme(getDefaultTheme().id)
        break
      case "noop":
        break
    }
  }, [hasPremiumAccess, isLoading, isError, query.data, parsed])

  return {
    customThemes: parsed.themes,
    errors: parsed.errors,
    directory: query.data?.directory ?? "",
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    // Surfaced so consumers can distinguish a failed load from an empty result.
    isError: query.isError,
    refetch: query.refetch,
  }
}
