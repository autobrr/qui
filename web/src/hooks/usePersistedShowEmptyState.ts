/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

/**
 * Hook to persist the show/hide empty items state in localStorage
 * Used for toggling visibility of empty statuses, categories, and tags in the filter sidebar
 */
export function usePersistedShowEmptyState(key: string, defaultValue: boolean = false) {
  const storageKey = `qui-show-empty-${key}`

  // Initialize state from localStorage or default value
  const [showEmpty, setShowEmpty] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        const parsed = JSON.parse(stored)
        return typeof parsed === "boolean" ? parsed : defaultValue
      }
    } catch (error) {
      console.error(`Failed to load show empty state from localStorage (${key}):`, error)
    }

    return defaultValue
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(showEmpty))
    } catch (error) {
      console.error(`Failed to save show empty state to localStorage (${key}):`, error)
    }
  }, [showEmpty, storageKey])

  return [showEmpty, setShowEmpty] as const
}
