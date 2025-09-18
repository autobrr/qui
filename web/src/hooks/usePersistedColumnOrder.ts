/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import type { ColumnOrderState } from "@tanstack/react-table"

export function usePersistedColumnOrder(
  defaultOrder: ColumnOrderState = []
) {
  // Global key shared across all instances
  const storageKey = "qui-column-order"

  // Initialize state from localStorage or default values
  const [columnOrder, setColumnOrder] = useState<ColumnOrderState>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        // Validate that it's an array of strings
        if (Array.isArray(parsed) && parsed.every(item => typeof item === "string")) {
          // Merge missing columns from defaultOrder into parsed order
          // This handles cases where new columns are added to the app
          const missingColumns = defaultOrder.filter(col => !parsed.includes(col))
          if (missingColumns.length > 0) {
            // Find the position where num_seeds and num_leechs should be inserted
            // They should come after "state" and before "dlspeed" based on typical column order
            const stateIndex = parsed.indexOf("state")
            const dlspeedIndex = parsed.indexOf("dlspeed")

            if (stateIndex !== -1 && dlspeedIndex !== -1) {
              // Insert missing columns between state and dlspeed
              const result = [...parsed]
              result.splice(stateIndex + 1, 0, ...missingColumns)
              return result
            } else {
              // Fallback: append missing columns at the end
              return [...parsed, ...missingColumns]
            }
          }
          return parsed
        }
      }
    } catch (error) {
      console.error("Failed to load column order from localStorage:", error)
    }

    return defaultOrder
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnOrder))
    } catch (error) {
      console.error("Failed to save column order to localStorage:", error)
    }
  }, [columnOrder, storageKey])

  return [columnOrder, setColumnOrder] as const
}