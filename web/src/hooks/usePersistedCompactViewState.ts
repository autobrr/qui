/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export type ViewMode = "normal" | "compact" | "ultra-compact"

export function usePersistedCompactViewState(defaultMode: ViewMode = "compact") {
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

  // Persist to localStorage whenever view mode changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, viewMode)
    } catch (error) {
      console.error("Failed to save view mode state to localStorage:", error)
    }
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
      const nextMode = prev === "normal" ? "compact" : 
                     prev === "compact" ? "ultra-compact" : "normal"
      
      try {
        localStorage.setItem(storageKey, nextMode)
      } catch (error) {
        console.error("Failed to save view mode state to localStorage:", error)
      }
      
      const evt = new CustomEvent(storageKey, { detail: { viewMode: nextMode } })
      window.dispatchEvent(evt)
      return nextMode
    })
  }

  return {
    viewMode,
    setViewMode: setViewModeState,
    cycleViewMode
  } as const
}
