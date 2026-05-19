/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"

export function usePersistedFilterSidebarState(defaultCollapsed: boolean = false) {
  const storageKey = "qui-filter-sidebar-collapsed"

  // Initialize state from localStorage or default value
  const [filterSidebarCollapsed, setFilterSidebarCollapsedState] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        return stored === "true"
      }
    } catch (error) {
      console.error("Failed to load filter sidebar state from localStorage:", error)
    }

    return defaultCollapsed
  })

  // Persist to localStorage and broadcast to other listeners whenever state changes
  useEffect(() => {
    if (typeof window === "undefined") return

    try {
      localStorage.setItem(storageKey, filterSidebarCollapsed.toString())
    } catch (error) {
      console.error("Failed to save filter sidebar state to localStorage:", error)
    }

    const evt = new CustomEvent(storageKey, { detail: { collapsed: filterSidebarCollapsed } })
    window.dispatchEvent(evt)
  }, [filterSidebarCollapsed])

  // Listen for cross-component updates via CustomEvent within the same tab
  useEffect(() => {
    const handleEvent = (e: Event) => {
      const custom = e as CustomEvent<{ collapsed: boolean }>
      if (typeof custom.detail?.collapsed === "boolean") {
        setFilterSidebarCollapsedState(custom.detail.collapsed)
      }
    }
    window.addEventListener(storageKey, handleEvent as EventListener)
    return () => window.removeEventListener(storageKey, handleEvent as EventListener)
  }, [])

  // Wrapped setter that syncs state and dispatches event
  const setFilterSidebarCollapsed = useCallback((next: boolean | ((prev: boolean) => boolean)) => {
    setFilterSidebarCollapsedState((prev) => (
      typeof next === "function" ? (next as (p: boolean) => boolean)(prev) : next
    ))
  }, [])

  return [filterSidebarCollapsed, setFilterSidebarCollapsed] as const
}
