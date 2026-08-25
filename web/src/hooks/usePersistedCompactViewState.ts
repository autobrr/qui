/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useMemo } from "react"

import { useClientSetting } from "@/lib/client-settings"

const STORAGE_KEY = "qui-torrent-view-mode"
const ALL_VIEW_MODES = ["normal", "dense", "compact", "ultra-compact"] as const

export type ViewMode = typeof ALL_VIEW_MODES[number]

function sanitizeAllowedModes(allowedModes?: readonly ViewMode[]): ViewMode[] {
  if (!allowedModes || allowedModes.length === 0) {
    return [...ALL_VIEW_MODES]
  }

  const deduped = Array.from(new Set(allowedModes))
  const filtered = deduped.filter(mode => ALL_VIEW_MODES.includes(mode))

  return filtered.length > 0 ? filtered : [...ALL_VIEW_MODES]
}

const parseViewMode = (raw: string): ViewMode => {
  if (ALL_VIEW_MODES.includes(raw as ViewMode)) return raw as ViewMode
  throw new Error("invalid view mode")
}

export function usePersistedCompactViewState(
  defaultMode: ViewMode = "normal",
  allowedModesInput?: readonly ViewMode[]
) {
  const allowedModes = useMemo(() => sanitizeAllowedModes(allowedModesInput), [allowedModesInput])
  const effectiveDefaultMode = allowedModes.includes(defaultMode) ? defaultMode : allowedModes[0]

  // The stored mode is global across every consumer; `allowedModes` only narrows what
  // this consumer can render. Never persist the narrowing, or a mounted-but-hidden
  // consumer (e.g. MobileFooterNav on desktop) clobbers the user's choice on reload.
  const [storedMode, setStoredMode] = useClientSetting<ViewMode>(STORAGE_KEY, {
    defaultValue: effectiveDefaultMode,
    parse: parseViewMode,
    serialize: String,
  })

  const viewMode = allowedModes.includes(storedMode) ? storedMode : effectiveDefaultMode

  const setViewMode = useCallback(
    (requested: ViewMode) => {
      setStoredMode(allowedModes.includes(requested) ? requested : allowedModes[0])
    },
    [allowedModes, setStoredMode]
  )

  const cycleViewMode = useCallback(() => {
    setViewMode(allowedModes[(allowedModes.indexOf(viewMode) + 1) % allowedModes.length])
  }, [allowedModes, setViewMode, viewMode])

  return {
    viewMode,
    setViewMode,
    cycleViewMode,
  } as const
}
