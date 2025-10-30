/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"

export function usePersistedDeleteFiles(defaultValue: boolean = false) {
  const storageKey = "qui-delete-files-default"
  const lockKey = "qui-delete-files-lock"

  const [isLocked, setIsLocked] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(lockKey)
      if (stored) {
        return JSON.parse(stored) === true
      }
    } catch (error) {
      console.error("Failed to read delete files lock state from localStorage:", error)
    }

    return false
  })

  // Initialize state from localStorage or default value (only when locked)
  const [deleteFiles, setDeleteFiles] = useState<boolean>(() => {
    try {
      const storedLock = localStorage.getItem(lockKey)
      const storedValue = localStorage.getItem(storageKey)

      if (storedLock && JSON.parse(storedLock) === true && storedValue) {
        return JSON.parse(storedValue)
      }
    } catch (error) {
      console.error("Failed to load delete files preference from localStorage:", error)
    }

    return defaultValue
  })

  // Persist the lock state and clear stored values when unlocking
  useEffect(() => {
    if (isLocked) {
      try {
        localStorage.setItem(lockKey, JSON.stringify(true))
      } catch (error) {
        console.error("Failed to update delete files lock state in localStorage:", error)
      }
      return
    }

    try {
      localStorage.removeItem(lockKey)
      localStorage.removeItem(storageKey)
    } catch (error) {
      console.error("Failed to clear delete files preference from localStorage:", error)
    }
  }, [isLocked, lockKey, storageKey])

  // Persist changes to the preference only when locked
  useEffect(() => {
    if (!isLocked) return

    try {
      localStorage.setItem(storageKey, JSON.stringify(deleteFiles))
    } catch (error) {
      console.error("Failed to save delete files preference to localStorage:", error)
    }
  }, [deleteFiles, isLocked, storageKey])

  const toggleLock = useCallback(() => {
    setIsLocked(prev => !prev)
  }, [])

  return {
    deleteFiles,
    setDeleteFiles,
    isLocked,
    toggleLock,
  } as const
}

