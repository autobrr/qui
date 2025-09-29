/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

export type ViewMode = "normal" | "compact" | "ultra-compact"

export function usePersistedCompactViewState(defaultMode: ViewMode = "normal") {
  const storageKey = "qui-torrent-view-mode"

  // Initialize state from localStorage or default value
  const [viewMode, setViewModeState] = useState<ViewMode>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored && ["normal", "compact", "ultra-compact"].includes(stored)) {
        return stored as ViewMode
      }
    } catch (error) {
      console.error("Failed to load view mode state from localStorage:", error)
    }

    return defaultMode
  })

  // Persist to localStorage and broadcast whenever view mode changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, viewMode)
    } catch (error) {
      console.error("Failed to save view mode state to localStorage:", error)
    }

    window.dispatchEvent(
      new CustomEvent(storageKey, {
        detail: { viewMode },
      })
    )
  }, [viewMode])

  // Listen for cross-component updates via CustomEvent within the same tab
  useEffect(() => {
    const handleEvent = (e: Event) => {
      const custom = e as CustomEvent<{ viewMode: ViewMode }>
      if (custom.detail?.viewMode && ["normal", "compact", "ultra-compact"].includes(custom.detail.viewMode)) {
        setViewModeState(custom.detail.viewMode)
      }
    }
    window.addEventListener(storageKey, handleEvent as EventListener)
    return () => window.removeEventListener(storageKey, handleEvent as EventListener)
  }, [])

  // Cycle through view modes: normal -> compact -> ultra-compact -> normal
  const cycleViewMode = () => {
    setViewModeState((prev: ViewMode) => {
      let nextMode: ViewMode

      if (prev === "normal") {
        nextMode = "compact"
      } else if (prev === "compact") {
        nextMode = "ultra-compact"
      } else {
        nextMode = "normal"
      }

      // Don't manually call localStorage.setItem here - the useEffect handles it

      return nextMode
    })
  }

  return {
    viewMode,
    setViewMode: setViewModeState,
    cycleViewMode,
  } as const
}
