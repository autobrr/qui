/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnOrderState } from "@tanstack/react-table"
import { useCallback, useMemo } from "react"

import { useClientSetting, useDropLegacyKey } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-column-order"

function mergeWithDefaults(order: unknown, defaultOrder: ColumnOrderState): ColumnOrderState {
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

export function usePersistedColumnOrder(
  defaultOrder: ColumnOrderState = [],
  instanceKey?: string | number
) {
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${BASE_STORAGE_KEY}:${instanceKey}` : BASE_STORAGE_KEY

  useDropLegacyKey(BASE_STORAGE_KEY, hasInstanceKey)

  const defaultsJson = JSON.stringify(defaultOrder)
  const defaultValue = useMemo<ColumnOrderState>(() => JSON.parse(defaultsJson), [defaultsJson])
  const parse = useCallback(
    (raw: string): ColumnOrderState => mergeWithDefaults(JSON.parse(raw), defaultValue),
    [defaultValue]
  )

  return useClientSetting<ColumnOrderState>(storageKey, { defaultValue, parse })
}
