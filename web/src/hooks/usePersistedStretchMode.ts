/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useState } from "react"

function storageKey(instanceId: number) {
  return instanceId > 0 ? `qui-stretch-name-column:${instanceId}` : "qui-stretch-name-column"
}

/**
 * Persists whether the Name column should stretch to fill the table (true)
 * or stay fixed-width so the table side-scrolls (false). Defaults to stretch.
 */
export function usePersistedStretchMode(instanceId: number): [boolean, () => void] {
  const key = storageKey(instanceId)

  const [stretch, setStretch] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(key)
      return stored === null ? true : stored !== "false"
    } catch {
      return true
    }
  })

  const toggle = useCallback(() => {
    setStretch((current) => {
      const next = !current
      try {
        localStorage.setItem(key, String(next))
      } catch {
        // ignore
      }
      return next
    })
  }, [key])

  return [stretch, toggle]
}
