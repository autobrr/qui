/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

export function usePersistedTabState<T extends string>(
  storageKey: string,
  defaultValue: T,
  isValid?: (value: string) => value is T
) {
  const [value, setValue] = useState<T>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored !== null) {
        if (!isValid || isValid(stored)) {
          return stored as T
        }
      }
    } catch (error) {
      console.error(`Failed to load tab state from localStorage (${storageKey}):`, error)
    }

    return defaultValue
  })

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, value)
    } catch (error) {
      console.error(`Failed to save tab state to localStorage (${storageKey}):`, error)
    }
  }, [storageKey, value])

  return [value, setValue] as const
}
