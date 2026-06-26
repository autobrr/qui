/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnSizingState } from "@tanstack/react-table"
import { useEffect, useState } from "react"

export function usePersistedColumnSizing(
  defaultSizing: ColumnSizingState = {},
  instanceKey?: string | number
) {
  const baseStorageKey = "qui-column-sizing"
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${baseStorageKey}:${instanceKey}` : baseStorageKey

  const parseSizing = (value: unknown): ColumnSizingState | undefined => {
    if (value && typeof value === "object" && !Array.isArray(value)) {
      const entries = Object.values(value as Record<string, unknown>)
      if (entries.every(entry => typeof entry === "number")) {
        return value as ColumnSizingState
      }
    }

    return undefined
  }

  const loadSizing = (): ColumnSizingState => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = parseSizing(JSON.parse(stored))
        if (parsed) {
          return parsed
        }
      }

      if (hasInstanceKey) {
        const legacyStored = localStorage.getItem(baseStorageKey)
        if (legacyStored) {
          const parsedLegacy = parseSizing(JSON.parse(legacyStored))
          if (parsedLegacy) {
            return parsedLegacy
          }
        }
      }
    } catch (error) {
      console.error("Failed to load column sizing from localStorage:", error)
    }

    return { ...defaultSizing }
  }

  const [columnSizing, setColumnSizing] = useState<ColumnSizingState>(() => loadSizing())

  useEffect(() => {
    if (!hasInstanceKey) {
      return
    }

    try {
      localStorage.removeItem(baseStorageKey)
    } catch (error) {
      console.error("Failed to clear legacy column sizing state:", error)
    }
  }, [hasInstanceKey, baseStorageKey])

  useEffect(() => {
    setColumnSizing(loadSizing())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnSizing))
    } catch (error) {
      console.error("Failed to save column sizing to localStorage:", error)
    }
  }, [columnSizing, storageKey])

  return [columnSizing, setColumnSizing] as const
}
