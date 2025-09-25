/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
  const legacyKeys = hasInstanceKey ? [baseStorageKey] : []

  const loadSorting = (): SortingState => {
    const keysToTry = [storageKey, ...legacyKeys]

    for (const key of keysToTry) {
      try {
        const stored = localStorage.getItem(key)
        if (stored) {
          const parsed = JSON.parse(stored)
          if (Array.isArray(parsed)) {
            if (key !== storageKey) {
              let migrationSucceeded = false

              try {
                localStorage.setItem(storageKey, stored)
                migrationSucceeded = true
              } catch (migrationError) {
                console.error("Failed to migrate legacy column sorting state:", migrationError)
              }

              if (migrationSucceeded) {
                try {
                  localStorage.removeItem(key)
                } catch (removeError) {
                  console.error("Failed to clear legacy column sorting state:", removeError)
                }
              }
            }
            return parsed as SortingState
          }
        }
      } catch (error) {
        console.error("Failed to load column sorting from localStorage:", error)
      }
    }

    return [...defaultSorting]
  }

  const [sorting, setSorting] = useState<SortingState>(() => loadSorting())

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
