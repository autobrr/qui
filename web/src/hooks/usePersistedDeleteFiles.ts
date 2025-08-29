/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export function usePersistedDeleteFiles(defaultValue: boolean = false) {
  const storageKey = "qui-delete-files-default"

  // Initialize state from localStorage or default value
  const [deleteFiles, setDeleteFiles] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        return JSON.parse(stored)
      }
    } catch (error) {
      console.error("Failed to load delete files preference from localStorage:", error)
    }

    return defaultValue
  })

  // Persist to localStorage whenever state changes
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(deleteFiles))
    } catch (error) {
      console.error("Failed to save delete files preference to localStorage:", error)
    }
  }, [deleteFiles, storageKey])

  return [deleteFiles, setDeleteFiles] as const
}