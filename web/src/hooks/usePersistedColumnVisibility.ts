/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from 'react'
import type { VisibilityState } from '@tanstack/react-table'

export function usePersistedColumnVisibility(
  defaultVisibility: VisibilityState = {}
) {
  // Global key shared across all instances
  const storageKey = `qui-column-visibility`
  
  // Initialize state from localStorage or default values
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error('Failed to load column visibility from localStorage:', error)
    }
    
    return defaultVisibility
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnVisibility))
    } catch (error) {
      console.error('Failed to save column visibility to localStorage:', error)
    }
  }, [columnVisibility, storageKey])
  
  return [columnVisibility, setColumnVisibility] as const
}