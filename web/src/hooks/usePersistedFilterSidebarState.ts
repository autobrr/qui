/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState, useEffect } from 'react'

export function usePersistedFilterSidebarState(defaultCollapsed: boolean = false) {
  const storageKey = 'qui-filter-sidebar-collapsed'
  
  // Initialize state from localStorage or default value
  const [filterSidebarCollapsed, setFilterSidebarCollapsed] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        return stored === 'true'
      }
    } catch (error) {
      console.error('Failed to load filter sidebar state from localStorage:', error)
    }
    
    return defaultCollapsed
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, filterSidebarCollapsed.toString())
    } catch (error) {
      console.error('Failed to save filter sidebar state to localStorage:', error)
    }
  }, [filterSidebarCollapsed])
  
  return [filterSidebarCollapsed, setFilterSidebarCollapsed] as const
}