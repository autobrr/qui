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
import { storedThemeId } from "@/lib/theme-storage"
import { setTheme, getAppliedCustomThemeCss } from "@/utils/theme"
import type { CustomThemeFile } from "@/lib/custom-themes"

const EMPTY: ParsedCustomThemes = { themes: [], errors: [] }
const CUSTOM_THEMES_KEY = ["custom-themes"]

export async function refreshCustomThemes(queryClient: QueryClient): Promise<CustomThemeFile[]> {
  await queryClient.cancelQueries({ queryKey: CUSTOM_THEMES_KEY })
  const data = await queryClient.fetchQuery({ queryKey: CUSTOM_THEMES_KEY, queryFn: () => api.getCustomThemes(), staleTime: 0 })
  registerCustomThemes(parseCustomThemes(data.themes).themes)
  return data.themes
}

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

  useEffect(() => {
    const themeId = storedThemeId()
    const action = decideCustomThemeAction({
      isLoading,
      isError,
      hasPremiumAccess,
      hasData: !!query.data,
      storedThemeId: themeId,
      themes: parsed.themes,
      appliedCustomCss: getAppliedCustomThemeCss(),
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
        registerCustomThemes(parsed.themes)
        void setTheme(themeId!, undefined, undefined, true)
        break
      case "register-and-downgrade":
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
    isError: query.isError,
    refetch: query.refetch,
  }
}
