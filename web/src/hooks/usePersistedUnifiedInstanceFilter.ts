/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useState } from "react"
import { encodeUnifiedInstanceIds, parseUnifiedInstanceIds } from "@/lib/instances"

const STORAGE_KEY = "qui-unified-instance-filter"

export function usePersistedUnifiedInstanceFilter(): [
  readonly number[],
  (ids: readonly number[]) => void,
] {
  const [persistedIds, setPersistedIds] = useState<readonly number[]>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY)
      return stored ? parseUnifiedInstanceIds(stored) : []
    } catch {
      return []
    }
  })

  const saveFilter = useCallback((ids: readonly number[]) => {
    setPersistedIds(ids.length > 0 ? [...ids] : [])
    try {
      const encoded = encodeUnifiedInstanceIds(ids)
      if (encoded) {
        localStorage.setItem(STORAGE_KEY, encoded)
      } else {
        localStorage.removeItem(STORAGE_KEY)
      }
    } catch (error) {
      console.error("Failed to save unified instance filter:", error)
    }
  }, [])

  return [persistedIds, saveFilter]
}
