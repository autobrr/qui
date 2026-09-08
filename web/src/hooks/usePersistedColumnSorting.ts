/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SortingState } from "@tanstack/react-table"
import { useCallback, useMemo } from "react"

import { useClientSetting, useDropLegacyKey } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-column-sorting"

export function usePersistedColumnSorting(
  defaultSorting: SortingState = [],
  instanceKey?: string | number
) {
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${BASE_STORAGE_KEY}:${instanceKey}` : BASE_STORAGE_KEY

  useDropLegacyKey(BASE_STORAGE_KEY, hasInstanceKey)

  const defaultsJson = JSON.stringify(defaultSorting)
  const defaultValue = useMemo<SortingState>(() => JSON.parse(defaultsJson), [defaultsJson])
  const parse = useCallback((raw: string): SortingState => {
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      return parsed as SortingState
    }
    throw new Error("invalid column sorting state")
  }, [])

  return useClientSetting<SortingState>(storageKey, { defaultValue, parse })
}
