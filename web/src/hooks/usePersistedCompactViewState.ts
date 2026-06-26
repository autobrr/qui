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

  const [viewMode, setViewModeState] = useState<ViewMode>(() => {
    if (typeof window === "undefined") {
      return effectiveDefaultMode
    }

    try {
      const stored = window.localStorage.getItem(STORAGE_KEY)
      if (stored && allowedModes.includes(stored as ViewMode)) {
        return stored as ViewMode
      }
    } catch (error) {
      console.error("Failed to load view mode state from localStorage:", error)
    }

    return effectiveDefaultMode
  })

  const setViewMode = useCallback((updater: ViewMode | ((prev: ViewMode) => ViewMode)) => {
    setViewModeState(prev => {
      const requested = typeof updater === "function" ? updater(prev) : updater
      return allowedModes.includes(requested) ? requested : allowedModes[0]
    })
  }, [allowedModes])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }

    try {
      window.localStorage.setItem(STORAGE_KEY, viewMode)
    } catch (error) {
      console.error("Failed to save view mode state to localStorage:", error)
    }

    const evt = new CustomEvent(STORAGE_KEY, { detail: { viewMode } })
    window.dispatchEvent(evt)
  }, [viewMode])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }

    const handleEvent = (e: Event) => {
      const custom = e as CustomEvent<{ viewMode: ViewMode }>
      const nextMode = custom.detail?.viewMode

      if (!nextMode) {
        return
      }

      if (allowedModes.includes(nextMode)) {
        setViewModeState(prev => (prev === nextMode ? prev : nextMode))
        return
      }

      if (allowedModes.length > 0) {
        setViewMode(allowedModes[0])
      }
    }

    window.addEventListener(STORAGE_KEY, handleEvent as EventListener)
    return () => window.removeEventListener(STORAGE_KEY, handleEvent as EventListener)
  }, [allowedModes, setViewMode])

  useEffect(() => {
    if (allowedModes.includes(viewMode)) {
      return
    }

    if (allowedModes.length > 0) {
      setViewMode(allowedModes[0])
    }
  }, [allowedModes, setViewMode, viewMode])

  const cycleViewMode = useCallback(() => {
    if (allowedModes.length === 0) {
      return
    }

    setViewMode(prev => {
      const currentIndex = allowedModes.indexOf(prev)
      const nextIndex = currentIndex === -1 ? 0 : (currentIndex + 1) % allowedModes.length
      return allowedModes[nextIndex]
    })
  }, [allowedModes, setViewMode])

  return {
    viewMode,
    setViewMode,
    cycleViewMode,
  } as const
}
