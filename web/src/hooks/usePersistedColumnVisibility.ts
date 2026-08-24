/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnVisibilityState } from "@tanstack/react-table"
import { useCallback, useMemo } from "react"

import { useClientSetting, useDropLegacyKey } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-column-visibility"

export function usePersistedColumnVisibility(
  defaultVisibility: ColumnVisibilityState = {},
  instanceKey?: string | number
) {
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${BASE_STORAGE_KEY}:${instanceKey}` : BASE_STORAGE_KEY

  useDropLegacyKey(BASE_STORAGE_KEY, hasInstanceKey)

  const defaultsJson = JSON.stringify(defaultVisibility)
  const defaultValue = useMemo<ColumnVisibilityState>(() => JSON.parse(defaultsJson), [defaultsJson])
  const parse = useCallback((raw: string): ColumnVisibilityState => {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as ColumnVisibilityState
    }
    throw new Error("invalid column visibility state")
  }, [])

  return useClientSetting<ColumnVisibilityState>(storageKey, { defaultValue, parse })
}
