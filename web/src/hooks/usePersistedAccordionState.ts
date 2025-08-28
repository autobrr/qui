/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export function usePersistedAccordionState(storageKey: string, defaultValue: string = "") {
  // Initialize state from localStorage or default value
  const [value, setValue] = useState<string>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        return stored
      }
    } catch (error) {
      console.error(`Failed to load accordion state from localStorage (${storageKey}):`, error)
    }
    
    return defaultValue
  })
  
  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, value)
    } catch (error) {
      console.error(`Failed to save accordion state to localStorage (${storageKey}):`, error)
    }
  }, [storageKey, value])
  
  return [value, setValue] as const
}