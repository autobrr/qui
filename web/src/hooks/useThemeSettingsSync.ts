/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { getThemeById } from "@/config/themes"
import { setTheme } from "@/utils/theme"
import type { ThemeSettings } from "@/types"

/**
 * Syncs the theme selection with the server. On load the stored server
 * selection is applied; afterwards every local theme change is pushed to the
 * server. localStorage stays as the instant-boot cache.
 */
export function useThemeSettingsSync(): void {
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
    if (!getThemeById(data.themeId)) return
    void setTheme(data.themeId, data.mode, data.variation, true)
  }, [data, builtins.isSuccess])

  // Push: store local theme changes on the server.
  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const { theme, mode, isSystemChange, variant } = (event as CustomEvent).detail
      if (isSystemChange) return
      // Push the stored selection, not the applied theme: a locked premium id
      // paints the fallback default, which must not overwrite the server
      // selection; mode changes still sync.
      const themeId = localStorage.getItem("color-theme") ?? theme.id
      const payload: ThemeSettings = { themeId, mode, ...(variant ? { variation: variant } : {}) }
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
