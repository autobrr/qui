/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export function usePersistedSidebarState(defaultCollapsed: boolean = false) {
  const storageKey = "qui-sidebar-collapsed"

  // Initialize state from localStorage or default value
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        return stored === "true"
      }
    } catch (error) {
      console.error("Failed to load sidebar state from localStorage:", error)
    }

    return defaultCollapsed
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, sidebarCollapsed.toString())
    } catch (error) {
      console.error("Failed to save sidebar state to localStorage:", error)
    }
  }, [sidebarCollapsed])

  return [sidebarCollapsed, setSidebarCollapsed] as const
}