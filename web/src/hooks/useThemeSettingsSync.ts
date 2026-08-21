/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import { getThemeById } from "@/config/themes"
import { setTheme } from "@/utils/theme"
import type { ThemeSettings } from "@/types"

/**
 * Syncs the theme selection with the server (premium-gated). On load the
 * stored server selection is applied; afterwards every local theme change is
 * pushed to the server. localStorage stays as the instant-boot cache.
 */
export function useThemeSettingsSync(): void {
  const { hasPremiumAccess } = useHasPremiumAccess()
  const builtins = useBuiltinThemes()
  // Last payload synced with the server, to avoid echoing an applied server
  // value straight back as a PUT.
  const lastSynced = useRef<string | null>(null)

  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
    enabled: hasPremiumAccess,
    // Poll so an API-side theme change repaints open tabs, hidden ones
    // included (desktop hooks PUT while qui is on another workspace).
    refetchInterval: 5_000,
    refetchIntervalInBackground: true,
    retry: false,
  })

  // Pull: apply the stored server selection. Re-runs when the async theme
  // registry lands, since the id may only resolve from then on.
  useEffect(() => {
    if (!data?.themeId) return
    lastSynced.current = JSON.stringify(data)
    // Skip unknown ids (e.g. a custom theme not registered yet) so we never
    // downgrade the local selection to the default theme.
    if (!getThemeById(data.themeId)) return
    void setTheme(data.themeId, data.mode, data.variation)
  }, [data, builtins.isSuccess])

  // Push: store local theme changes on the server.
  useEffect(() => {
    if (!hasPremiumAccess) return

    const handleThemeChange = (event: Event) => {
      const { theme, mode, isSystemChange, variant } = (event as CustomEvent).detail
      if (isSystemChange) return
      const payload: ThemeSettings = { themeId: theme.id, mode, ...(variant ? { variation: variant } : {}) }
      const serialized = JSON.stringify(payload)
      if (serialized === lastSynced.current) return
      lastSynced.current = serialized
      api.updateThemeSettings(payload).catch(() => {
        lastSynced.current = null
      })
    }

    window.addEventListener("themechange", handleThemeChange)
    return () => window.removeEventListener("themechange", handleThemeChange)
  }, [hasPremiumAccess])
}
