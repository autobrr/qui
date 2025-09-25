/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnOrderState } from "@tanstack/react-table"
import { useEffect, useState } from "react"

export function usePersistedColumnOrder(
  defaultOrder: ColumnOrderState = [],
  instanceKey?: string | number
) {
  const baseStorageKey = "qui-column-order"
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${baseStorageKey}:${instanceKey}` : baseStorageKey
  const legacyKeys = hasInstanceKey ? [baseStorageKey] : []

  const mergeWithDefaults = (order: ColumnOrderState): ColumnOrderState => {
    if (!Array.isArray(order) || order.some(item => typeof item !== "string")) {
      return [...defaultOrder]
    }

    const missingColumns = defaultOrder.filter(col => !order.includes(col))
    if (missingColumns.length === 0) {
      return [...order]
    }

    const stateIndex = order.indexOf("state")
    const dlspeedIndex = order.indexOf("dlspeed")

    if (stateIndex !== -1 && dlspeedIndex !== -1 && dlspeedIndex >= stateIndex) {
      const result = [...order]
      result.splice(stateIndex + 1, 0, ...missingColumns)
      return result
    }

    return [...order, ...missingColumns]
  }

  const loadOrder = (): ColumnOrderState => {
    const keysToTry = [storageKey, ...legacyKeys]

    for (const key of keysToTry) {
      try {
        const stored = localStorage.getItem(key)
        if (stored) {
          const parsed = JSON.parse(stored)
          const merged = mergeWithDefaults(parsed)

          if (key !== storageKey) {
            let migrationSucceeded = false

            try {
              localStorage.setItem(storageKey, JSON.stringify(merged))
              migrationSucceeded = true
            } catch (migrationError) {
              console.error("Failed to migrate legacy column order state:", migrationError)
            }

            if (migrationSucceeded) {
              try {
                localStorage.removeItem(key)
              } catch (removeError) {
                console.error("Failed to clear legacy column order state:", removeError)
              }
            }
          }

          return merged
        }
      } catch (error) {
        console.error("Failed to load column order from localStorage:", error)
      }
    }

    return [...defaultOrder]
  }

  const [columnOrder, setColumnOrder] = useState<ColumnOrderState>(() => loadOrder())

  useEffect(() => {
    setColumnOrder(loadOrder())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey, JSON.stringify(defaultOrder)])

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnOrder))
    } catch (error) {
      console.error("Failed to save column order to localStorage:", error)
    }
  }, [columnOrder, storageKey])

  return [columnOrder, setColumnOrder] as const
}
