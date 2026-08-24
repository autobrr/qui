/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnSizingState } from "@tanstack/react-table"
import { useCallback, useEffect, useMemo } from "react"

import { readRaw, useClientSetting, writeRaw } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-column-sizing"

function parseSizing(value: unknown): ColumnSizingState | undefined {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const entries = Object.values(value as Record<string, unknown>)
    if (entries.every(entry => typeof entry === "number")) {
      return value as ColumnSizingState
    }
  }

  return undefined
}

export function usePersistedColumnSizing(
  defaultSizing: ColumnSizingState = {},
  instanceKey?: string | number
) {
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${BASE_STORAGE_KEY}:${instanceKey}` : BASE_STORAGE_KEY

  // Migrate the pre-instance-scoping shared key into the instance key once,
  // then drop it.
  useEffect(() => {
    if (!hasInstanceKey) return
    try {
      const legacy = localStorage.getItem(BASE_STORAGE_KEY)
      if (legacy && readRaw(storageKey) == null) {
        if (parseSizing(JSON.parse(legacy))) writeRaw(storageKey, legacy)
      }
      localStorage.removeItem(BASE_STORAGE_KEY)
    } catch (error) {
      console.error("Failed to migrate legacy column sizing state:", error)
    }
  }, [hasInstanceKey, storageKey])

  const defaultsJson = JSON.stringify(defaultSizing)
  const defaultValue = useMemo<ColumnSizingState>(() => JSON.parse(defaultsJson), [defaultsJson])
  const parse = useCallback((raw: string): ColumnSizingState => {
    const parsed = parseSizing(JSON.parse(raw))
    if (!parsed) throw new Error("invalid column sizing state")
    return parsed
  }, [])

  return useClientSetting<ColumnSizingState>(storageKey, { defaultValue, parse })
}
