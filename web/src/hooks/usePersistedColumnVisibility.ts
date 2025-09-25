/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { VisibilityState } from "@tanstack/react-table"
import { useEffect, useState } from "react"

export function usePersistedColumnVisibility(
  defaultVisibility: VisibilityState = {},
  instanceKey?: string | number
) {
  const baseStorageKey = "qui-column-visibility"
  const hasInstanceKey = instanceKey !== undefined && instanceKey !== null
  const storageKey = hasInstanceKey ? `${baseStorageKey}:${instanceKey}` : baseStorageKey
  const legacyKeys = hasInstanceKey ? [baseStorageKey] : []

  const loadVisibility = (): VisibilityState => {
    const keysToTry = [storageKey, ...legacyKeys]

    for (const key of keysToTry) {
      try {
        const stored = localStorage.getItem(key)
        if (stored) {
          const parsed = JSON.parse(stored)
          if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
            if (key !== storageKey) {
              let migrationSucceeded = false

              try {
                localStorage.setItem(storageKey, stored)
                migrationSucceeded = true
              } catch (migrationError) {
                console.error("Failed to migrate legacy column visibility state:", migrationError)
              }

              if (migrationSucceeded) {
                try {
                  localStorage.removeItem(key)
                } catch (removeError) {
                  console.error("Failed to clear legacy column visibility state:", removeError)
                }
              }
            }
            return parsed as VisibilityState
          }
        }
      } catch (error) {
        console.error("Failed to load column visibility from localStorage:", error)
      }
    }

    return { ...defaultVisibility }
  }

  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(() => loadVisibility())

  useEffect(() => {
    setColumnVisibility(loadVisibility())
    // We only want to reload when the storage key changes (instance switch)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey])

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnVisibility))
    } catch (error) {
      console.error("Failed to save column visibility to localStorage:", error)
    }
  }, [columnVisibility, storageKey])

  return [columnVisibility, setColumnVisibility] as const
}
