/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

/**
 * Hook to persist the "Start Torrents Paused" preference per instance in localStorage
 *
 * NOTE: This is a workaround for qBittorrent's API limitation where the start_paused_enabled
 * preference cannot be set via app/setPreferences (it gets rejected/ignored). Instead of
 * relying on qBittorrent's global preference, we store this setting in localStorage and
 * apply it when adding torrents.
 */
export function usePersistedStartPaused(instanceId: number, defaultValue: boolean = false) {
  const storageKey = `qui-start-paused-instance-${instanceId}`

  // Initialize state from localStorage or default value
  const [startPaused, setStartPaused] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error("Failed to load start paused preference from localStorage:", error)
    }

    return defaultValue
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(startPaused))
    } catch (error) {
      console.error("Failed to save start paused preference to localStorage:", error)
    }
  }, [startPaused, storageKey])

  return [startPaused, setStartPaused] as const
}