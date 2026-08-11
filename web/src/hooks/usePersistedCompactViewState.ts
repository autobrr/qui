/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useState } from "react"

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

export function usePersistedCompactViewState(
  defaultMode: ViewMode = "normal",
  allowedModesInput?: readonly ViewMode[]
) {
  const allowedModes = useMemo(() => sanitizeAllowedModes(allowedModesInput), [allowedModesInput])
  const effectiveDefaultMode = allowedModes.includes(defaultMode) ? defaultMode : allowedModes[0]

  // The stored mode is global across every consumer; `allowedModes` only narrows what
  // this consumer can render. Never persist the narrowing, or a mounted-but-hidden
  // consumer (e.g. MobileFooterNav on desktop) clobbers the user's choice on reload.
  const [storedMode, setStoredMode] = useState<ViewMode>(() => {
    if (typeof window === "undefined") {
      return effectiveDefaultMode
    }

    try {
      const stored = window.localStorage.getItem(STORAGE_KEY)
      if (stored && ALL_VIEW_MODES.includes(stored as ViewMode)) {
        return stored as ViewMode
      }
    } catch (error) {
      console.error("Failed to load view mode state from localStorage:", error)
    }

    return effectiveDefaultMode
  })

  const viewMode = allowedModes.includes(storedMode) ? storedMode : effectiveDefaultMode

  const setViewMode = useCallback((requested: ViewMode) => {
    const next = allowedModes.includes(requested) ? requested : allowedModes[0]

    setStoredMode(next)

    if (typeof window === "undefined") {
      return
    }

    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch (error) {
      console.error("Failed to save view mode state to localStorage:", error)
    }

    window.dispatchEvent(new CustomEvent(STORAGE_KEY, { detail: { viewMode: next } }))
  }, [allowedModes])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }

    const handleEvent = (e: Event) => {
      const nextMode = (e as CustomEvent<{ viewMode: ViewMode }>).detail?.viewMode

      if (nextMode && ALL_VIEW_MODES.includes(nextMode)) {
        setStoredMode(nextMode)
      }
    }

    window.addEventListener(STORAGE_KEY, handleEvent as EventListener)
    return () => window.removeEventListener(STORAGE_KEY, handleEvent as EventListener)
  }, [])

  const cycleViewMode = useCallback(() => {
    setViewMode(allowedModes[(allowedModes.indexOf(viewMode) + 1) % allowedModes.length])
  }, [allowedModes, setViewMode, viewMode])

  return {
    viewMode,
    setViewMode,
    cycleViewMode,
  } as const
}
