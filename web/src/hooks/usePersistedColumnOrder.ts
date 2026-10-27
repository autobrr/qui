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

  const result = [...order]

  // A missing column goes right after its nearest default-order predecessor, so several
  // missing columns keep their default order and a column with no predecessor goes first.
  defaultOrder.forEach((columnId, defaultIndex) => {
    if (result.includes(columnId)) return

    let insertAt = 0
    for (let i = defaultIndex - 1; i >= 0; i--) {
      const predecessorIndex = result.indexOf(defaultOrder[i])
      if (predecessorIndex !== -1) {
        insertAt = predecessorIndex + 1
        break
      }
    }
    result.splice(insertAt, 0, columnId)
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
