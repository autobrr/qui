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
import { isDefaultThemeVisit, storedThemeId, storeThemeId, useDefaultThemeVisit } from "@/lib/theme-storage"
import { setTheme, type ThemeMode } from "@/utils/theme"
import type { ThemeSettings, ThemeSlot } from "@/types"

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

export function useThemeLocalOnly(): boolean {
  return useSyncExternalStore(subscribeLocalOnly, readLocalOnly)
}

export function useCanWriteThemeSlots(): boolean {
  const localOnly = useThemeLocalOnly()
  const visit = useDefaultThemeVisit()
  return !localOnly && !visit
}

function canWriteThemeSlots(): boolean {
  return !readLocalOnly() && !isDefaultThemeVisit()
}

export async function writeThemeSlot(selection: ThemeSettings, slot: ThemeSlot): Promise<void> {
  if (!canWriteThemeSlots()) return
  await api.updateThemeSettings(selection, slot)
}

export async function removeMobileThemeSlot(): Promise<void> {
  if (!canWriteThemeSlots()) return
  await api.deleteMobileThemeSettings()
}

export function setThemeLocalOnly(localOnly: boolean): void {
  if (localOnly) localStorage.setItem(LOCAL_ONLY_KEY, "true")
  else localStorage.removeItem(LOCAL_ONLY_KEY)
  window.dispatchEvent(new Event(LOCAL_ONLY_EVENT))
}

export function storedThemeSelection(mode: ThemeMode, appliedId: string, appliedVariant?: string): ThemeSettings {
  const themeId = storedThemeId() ?? appliedId
  const variation = themeId === appliedId ? appliedVariant : getStoredVariation(themeId)
  return { themeId, mode, ...(variation ? { variation } : {}) }
}

export function useThemeSettingsSync(): void {
  const queryClient = useQueryClient()
  const builtins = useBuiltinThemes()
  const isMobile = useIsMobile()
  const localOnly = useThemeLocalOnly()
  const lastSynced = useRef<string | null>(null)

  const isAuthed = useIsAuthed()

  useActivityStream(isAuthed)

  const { data } = useQuery({
    queryKey: ["theme-settings"],
    queryFn: () => api.getThemeSettings(),
    refetchInterval: isAuthed ? false : 5_000,
    retry: false,
  })

  const slot: ThemeSlot | null =
    localOnly || (isMobile && !data) ? null : isMobile && data?.mobile ? "mobile" : "default"
  const selection = slot ? data?.[slot] : undefined
  const slotRef = useRef(slot)
  slotRef.current = slot

  useEffect(() => {
    if (isAuthed) void queryClient.invalidateQueries({ queryKey: ["theme-settings"] })
  }, [isAuthed, queryClient])

  useEffect(() => {
    if (!selection?.themeId || isDefaultThemeVisit()) return
    lastSynced.current = JSON.stringify(selection)
    storeThemeId(selection.themeId)
    const resolved = getThemeById(selection.themeId)
    if (!resolved) return
    if (resolved.locked) {
      void queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
    }
    void setTheme(selection.themeId, selection.mode, selection.variation, true)
  }, [selection, builtins.isSuccess, queryClient])

  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const { theme, mode, isSystemChange, variant } = (event as CustomEvent).detail
      const slot = slotRef.current
      if (isSystemChange || !slot || !canWriteThemeSlots()) return
      const payload = storedThemeSelection(mode, theme.id, variant)
      const serialized = JSON.stringify(payload)
      if (serialized === lastSynced.current) return
      lastSynced.current = serialized
      writeThemeSlot(payload, slot).catch(() => {
        lastSynced.current = null
      })
    }

    window.addEventListener("themechange", handleThemeChange)
    return () => window.removeEventListener("themechange", handleThemeChange)
  }, [])
}
