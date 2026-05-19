/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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

  const mergeWithDefaults = (order: ColumnOrderState): ColumnOrderState => {
    if (!Array.isArray(order) || order.some(item => typeof item !== "string")) {
      return [...defaultOrder]
    }

    const missingColumns = defaultOrder.filter(col => !order.includes(col))
    if (missingColumns.length === 0) {
      return [...order]
    }

    const result = [...order]

    missingColumns.forEach(columnId => {
      if (columnId === "tracker_icon" || columnId === "status_icon") {
        const priorityIndex = result.indexOf("priority")
        if (priorityIndex !== -1) {
          result.splice(priorityIndex + 1, 0, columnId)
          return
        }
      }

      const stateIndex = result.indexOf("state")
      const dlspeedIndex = result.indexOf("dlspeed")
      if (stateIndex !== -1 && dlspeedIndex !== -1 && columnId !== "tracker_icon" && columnId !== "status_icon") {
        result.splice(stateIndex + 1, 0, columnId)
      } else {
        result.push(columnId)
      }
    })

    return result
  }

  const loadOrder = (): ColumnOrderState => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        return mergeWithDefaults(parsed)
      }
    } catch (error) {
      console.error("Failed to load column order from localStorage:", error)
    }

    return [...defaultOrder]
  }

  const [columnOrder, setColumnOrder] = useState<ColumnOrderState>(() => loadOrder())

  useEffect(() => {
    if (!hasInstanceKey) {
      return
    }

    try {
      localStorage.removeItem(baseStorageKey)
    } catch (error) {
      console.error("Failed to clear legacy column order state:", error)
    }
  }, [hasInstanceKey, baseStorageKey])

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
