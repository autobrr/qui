/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"

export function usePersistedDeleteFiles(defaultValue: boolean = false) {
  const storageKey = "qui-delete-files-default"
  const lockKey = "qui-delete-files-lock"

  const readStoredPreference = () => {
    try {
      const existingPreference = localStorage.getItem(storageKey)
      if (existingPreference) {
        const parsedPreference = JSON.parse(existingPreference)
        if (typeof parsedPreference === "boolean") {
          return parsedPreference
        }
      }
    } catch (error) {
      console.error("Failed to read delete files preference from localStorage:", error)
    }

    return undefined
  }

  const readStoredLock = () => {
    try {
      const storedLock = localStorage.getItem(lockKey)
      if (storedLock) {
        return JSON.parse(storedLock) === true
      }

      const preference = readStoredPreference()
      return typeof preference === "boolean"
    } catch (error) {
      console.error("Failed to read delete files lock state from localStorage:", error)
    }

    return false
  }

  const [isLocked, setIsLocked] = useState<boolean>(() => readStoredLock())

  // Initialize state from localStorage or default value
  const [deleteFiles, setDeleteFiles] = useState<boolean>(() => readStoredPreference() ?? defaultValue)

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
