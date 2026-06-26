/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import { parseCustomThemes, isCustomThemeId, type ParsedCustomThemes } from "@/lib/custom-themes"
import { registerCustomThemes, getDefaultTheme } from "@/config/themes"
import { setTheme } from "@/utils/theme"

const EMPTY: ParsedCustomThemes = { themes: [], errors: [] }

/**
 * Loads sideloaded custom themes (premium-gated). It fetches the directory
 * listing only when the user has premium access, parses each file with the
 * shared theme parser, registers the results so they resolve in the picker,
 * and re-applies / downgrades the stored selection as the registry changes.
 */
export function useCustomThemes() {
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess()

  const query = useQuery({
    queryKey: ["custom-themes"],
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
    // While the license check is in flight, or on a transient error, leave the
    // registry and stored selection untouched. Downgrading here would clobber a
    // premium user's stored custom theme before their license even resolves.
    if (isLoading || isError) return

    if (!hasPremiumAccess) {
      // Confirmed no premium: drop custom themes and downgrade a stored custom
      // selection to the default free theme.
      registerCustomThemes([])
      if (isCustomThemeId(localStorage.getItem("color-theme"))) {
        void setTheme(getDefaultTheme().id)
      }
      return
    }

    if (!query.data) return

    registerCustomThemes(parsed.themes)

    const storedId = localStorage.getItem("color-theme")
    if (!isCustomThemeId(storedId)) return

    if (!parsed.themes.some(theme => theme.id === storedId)) {
      // The selected custom theme's file was removed/renamed - fall back.
      void setTheme(getDefaultTheme().id)
      return
    }

    // Apply the stored custom theme now that it is registered; it could not be
    // resolved synchronously during the initial (pre-fetch) theme init.
    if (document.documentElement.getAttribute("data-theme") !== storedId) {
      void setTheme(storedId!)
    }
  }, [hasPremiumAccess, isLoading, isError, query.data, parsed])

  return {
    customThemes: parsed.themes,
    errors: parsed.errors,
    directory: query.data?.directory ?? "",
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    refetch: query.refetch,
  }
}
