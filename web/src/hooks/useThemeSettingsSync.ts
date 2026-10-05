/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef, useSyncExternalStore } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useIsAuthed } from "@/hooks/useIsAuthed"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { getStoredVariation } from "@/hooks/usePersistedThemeVariation"
import { getThemeById } from "@/config/themes"
import { setTheme, type ThemeMode } from "@/utils/theme"
import type { ThemeSettings, ThemeSlot } from "@/types"

// Browser-only on purpose: it is not a synced client setting, because a synced
// flag would apply to every browser.
const LOCAL_ONLY_KEY = "qui-theme-local-only"
const LOCAL_ONLY_EVENT = "qui-theme-local-only-changed"

function subscribeLocalOnly(callback: () => void): () => void {
  window.addEventListener("storage", callback)
  window.addEventListener(LOCAL_ONLY_EVENT, callback)
  return () => {
    window.removeEventListener("storage", callback)
    window.removeEventListener(LOCAL_ONLY_EVENT, callback)
  }
}

function readLocalOnly(): boolean {
  try {
    return localStorage.getItem(LOCAL_ONLY_KEY) === "true"
  } catch {
    return false
  }
}

/** Whether this browser keeps its theme local and never syncs it with the server. */
export function useThemeLocalOnly(): boolean {
  return useSyncExternalStore(subscribeLocalOnly, readLocalOnly)
}

export function setThemeLocalOnly(localOnly: boolean): void {
  if (localOnly) localStorage.setItem(LOCAL_ONLY_KEY, "true")
  else localStorage.removeItem(LOCAL_ONLY_KEY)
  window.dispatchEvent(new Event(LOCAL_ONLY_EVENT))
}

/**
 * The stored selection, not the applied theme: a locked premium id paints the
 * fallback default, which must not overwrite the server selection.
 */
export function storedThemeSelection(mode: ThemeMode, appliedId: string, appliedVariant?: string): ThemeSettings {
  const themeId = localStorage.getItem("color-theme") ?? appliedId
  const variation = themeId === appliedId ? appliedVariant : getStoredVariation(themeId)
  return { themeId, mode, ...(variation ? { variation } : {}) }
}

/**
 * Syncs the theme selection with one server theme slot. The tab uses no slot
 * when this browser keeps its theme local, the mobile slot when it exists and
 * the layout is mobile, and the default slot otherwise. The slot's selection
 * is applied on load and when the slot changes; afterwards every local theme
 * change is pushed to that slot. localStorage stays as the instant-boot cache.
 */
export function useThemeSettingsSync(): void {
  const queryClient = useQueryClient()
  const builtins = useBuiltinThemes()
  const isMobile = useIsMobile()
  const localOnly = useThemeLocalOnly()
  // Last payload synced with the server, to avoid echoing an applied server
  // value straight back as a PUT.
  const lastSynced = useRef<string | null>(null)

  // Pre-auth the activity stream would 401 and retry forever, so
  // registration must wait for a user.
  const isAuthed = useIsAuthed()

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

  const slot: ThemeSlot | null = localOnly ? null : data?.mobile && isMobile ? "mobile" : "default"
  const selection = slot ? data?.[slot] : undefined
  const slotRef = useRef(slot)
  slotRef.current = slot

  // Stopping the pre-auth poll does not itself fetch after login.
  useEffect(() => {
    if (isAuthed) void queryClient.invalidateQueries({ queryKey: ["theme-settings"] })
  }, [isAuthed, queryClient])

  // Pull: apply the selection of the current slot. Re-runs when the slot
  // changes, and when the async theme registry lands, since the id may only
  // resolve from then on.
  useEffect(() => {
    if (!selection?.themeId) return
    lastSynced.current = JSON.stringify(selection)
    // Mirror the server selection locally even when it resolves to a locked
    // stub or an unknown id, so the push below never sends a differing local
    // id over it.
    localStorage.setItem("color-theme", selection.themeId)
    // Skip unknown ids (e.g. a custom theme not registered yet) so we never
    // downgrade the local selection to the default theme.
    const resolved = getThemeById(selection.themeId)
    if (!resolved) return
    if (resolved.locked) {
      // The cached catalog carries CSS only for the previously selected
      // theme, but the server serves the selected theme's CSS even pre-auth.
      // Refetch so a remote selection change can paint instead of sitting on
      // the default until the hourly refresh.
      void queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    }
    void setTheme(selection.themeId, selection.mode, selection.variation, true)
  }, [selection, builtins.isSuccess, queryClient])

  // Push: store local theme changes in the current slot.
  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const { theme, mode, isSystemChange, variant } = (event as CustomEvent).detail
      const slot = slotRef.current
      if (isSystemChange || !slot) return
      // Mode changes during a locked fallback still sync.
      const payload = storedThemeSelection(mode, theme.id, variant)
      const serialized = JSON.stringify(payload)
      if (serialized === lastSynced.current) return
      lastSynced.current = serialized
      api.updateThemeSettings(payload, slot).catch(() => {
        lastSynced.current = null
      })
    }

    window.addEventListener("themechange", handleThemeChange)
    return () => window.removeEventListener("themechange", handleThemeChange)
  }, [])
}
