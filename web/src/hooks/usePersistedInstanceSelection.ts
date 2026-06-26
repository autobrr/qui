/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

export function usePersistedInstanceSelection(storageNamespace: string) {
  const storageKey = `qui-selected-instance-${storageNamespace}`

  const [selectedInstanceId, setSelectedInstanceId] = useState<number | undefined>(() => {
    try {
      const raw = localStorage.getItem(storageKey)
      if (!raw) return undefined
      const parsed = JSON.parse(raw)
      return typeof parsed === "number" ? parsed : undefined
    } catch {
      return undefined
    }
  })

  useEffect(() => {
    try {
      if (typeof selectedInstanceId === "number") {
        localStorage.setItem(storageKey, JSON.stringify(selectedInstanceId))
      } else {
        localStorage.removeItem(storageKey)
      }
    } catch (error) {
      console.error("Failed to persist selected instance:", error)
    }
  }, [selectedInstanceId, storageKey])

  return [selectedInstanceId, setSelectedInstanceId] as const
}
