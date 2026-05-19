/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SortingState } from "@tanstack/react-table"
import { useEffect, useState } from "react"

export function usePersistedColumnSorting(
  defaultSorting: SortingState = [],
  instanceKey?: string | number
) {
  const baseStorageKey = "qui-column-sorting"
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${baseStorageKey}:${instanceKey}` : baseStorageKey

  const loadSorting = (): SortingState => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        if (Array.isArray(parsed)) {
          return parsed as SortingState
        }
      }
    } catch (error) {
      console.error("Failed to load column sorting from localStorage:", error)
    }

    return [...defaultSorting]
  }

  const [sorting, setSorting] = useState<SortingState>(() => loadSorting())

  useEffect(() => {
    if (!hasInstanceKey) {
      return
    }

    try {
      localStorage.removeItem(baseStorageKey)
    } catch (error) {
      console.error("Failed to clear legacy column sorting state:", error)
    }
  }, [hasInstanceKey, baseStorageKey])

  useEffect(() => {
    setSorting(loadSorting())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(sorting))
    } catch (error) {
      console.error("Failed to save column sorting to localStorage:", error)
    }
  }, [sorting, storageKey])

  return [sorting, setSorting] as const
}
