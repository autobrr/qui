/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState, useEffect } from 'react'
import type { ColumnSizingState } from '@tanstack/react-table'

export function usePersistedColumnSizing(
  defaultSizing: ColumnSizingState = {}
) {
  // Global key shared across all instances
  const storageKey = `qui-column-sizing`
  
  // Initialize state from localStorage or default values
  const [columnSizing, setColumnSizing] = useState<ColumnSizingState>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        // Validate that it's an object with numeric values
        if (typeof parsed === 'object' && parsed !== null) {
          const isValid = Object.values(parsed).every(val => typeof val === 'number')
          if (isValid) {
            return parsed
          }
        }
      }
    } catch (error) {
      console.error('Failed to load column sizing from localStorage:', error)
    }
    
    return defaultSizing
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnSizing))
    } catch (error) {
      console.error('Failed to save column sizing to localStorage:', error)
    }
  }, [columnSizing, storageKey])
  
  return [columnSizing, setColumnSizing] as const
}