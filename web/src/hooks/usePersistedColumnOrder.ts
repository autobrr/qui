/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState, useEffect } from 'react'
import type { ColumnOrderState } from '@tanstack/react-table'

export function usePersistedColumnOrder(
  defaultOrder: ColumnOrderState = []
) {
  // Global key shared across all instances
  const storageKey = `qui-column-order`
  
  // Initialize state from localStorage or default values
  const [columnOrder, setColumnOrder] = useState<ColumnOrderState>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        // Validate that it's an array of strings
        if (Array.isArray(parsed) && parsed.every(item => typeof item === 'string')) {
          return parsed
        }
      }
    } catch (error) {
      console.error('Failed to load column order from localStorage:', error)
    }
    
    return defaultOrder
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnOrder))
    } catch (error) {
      console.error('Failed to save column order to localStorage:', error)
    }
  }, [columnOrder, storageKey])
  
  return [columnOrder, setColumnOrder] as const
}