/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { getStoredVariation } from "@/hooks/usePersistedThemeVariation"
import { getThemeById } from "@/config/themes"
import { setTheme } from "@/utils/theme"
import type { ThemeSettings } from "@/types"

/**
 * Syncs the theme selection with the server. On load the stored server
 * selection is applied; afterwards every local theme change is pushed to the
 * server. localStorage stays as the instant-boot cache.
 */
export function useThemeSettingsSync(): void {
  const queryClient = useQueryClient()
  const builtins = useBuiltinThemes()
  // Last payload synced with the server, to avoid echoing an applied server
  // value straight back as a PUT.
  const lastSynced = useRef<string | null>(null)

  // Observe the cached auth state without fetching (enabled: false); useAuth
  // owns the fetch. Pre-auth the activity stream would 401 and retry forever,
  // so registration must wait for a user.
  const { data: user } = useQuery({
    queryKey: ["auth", "user"],
    queryFn: () => api.checkAuth(),
    enabled: false,
  })
  const isAuthed = Boolean(user)

  // Authed tabs hear about API-side theme changes over the activity SSE
  // channel ("theme.settings" invalidates ["theme-settings"]); the login/setup
  // page cannot ride the auth-gated stream, so it keeps the poll.
  useActivityStream(isAuthed)

  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
    refetchInterval: isAuthed ? false : 5_000,
    retry: false,
  })

  // Stopping the pre-auth poll does not itself fetch after login.
  useEffect(() => {
    if (isAuthed) void queryClient.invalidateQueries({ queryKey: ["theme-settings"] })
  }, [isAuthed, queryClient])

  // Pull: apply the stored server selection. Re-runs when the async theme
  // registry lands, since the id may only resolve from then on.
  useEffect(() => {
    if (!data?.themeId) return
    lastSynced.current = JSON.stringify(data)
    // Mirror the server selection locally even when it resolves to a locked
    // stub or an unknown id, so the push below never sends a differing local
    // id over it.
    localStorage.setItem("color-theme", data.themeId)
    // Skip unknown ids (e.g. a custom theme not registered yet) so we never
    // downgrade the local selection to the default theme.
    const resolved = getThemeById(data.themeId)
    if (!resolved) return
    if (resolved.locked) {
      // The cached catalog carries CSS only for the previously selected
      // theme, but the server serves the selected theme's CSS even pre-auth.
      // Refetch so a remote selection change can paint instead of sitting on
      // the default until the hourly refresh.
      void queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    }
    void setTheme(data.themeId, data.mode, data.variation, true)
  }, [data, builtins.isSuccess, queryClient])

  // Push: store local theme changes on the server.
  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const { theme, mode, isSystemChange, variant } = (event as CustomEvent).detail
      if (isSystemChange) return
      // Push the stored selection, not the applied theme: a locked premium id
      // paints the fallback default, which must not overwrite the server
      // selection; mode changes still sync.
      const themeId = localStorage.getItem("color-theme") ?? theme.id
      const variation = themeId === theme.id ? variant : getStoredVariation(themeId)
      const payload: ThemeSettings = { themeId, mode, ...(variation ? { variation } : {}) }
      const serialized = JSON.stringify(payload)
      if (serialized === lastSynced.current) return
      lastSynced.current = serialized
      api.updateThemeSettings(payload).catch(() => {
        lastSynced.current = null
      })
    }

    window.addEventListener("themechange", handleThemeChange)
    return () => window.removeEventListener("themechange", handleThemeChange)
  }, [])
}
