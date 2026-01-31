/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

const STORAGE_KEY = "qui-titlebar-speeds-enabled"

export function usePersistedTitleBarSpeeds(defaultValue: boolean = false) {
  const [isEnabled, setIsEnabled] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY)
      if (stored !== null) {
        const parsed = JSON.parse(stored)
        if (typeof parsed === "boolean") {
          return parsed
        }
      }
    } catch (error) {
      console.error("Failed to load title bar speed preference from localStorage:", error)
    }

    return defaultValue
  })

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(isEnabled))
    } catch (error) {
      console.error("Failed to save title bar speed preference to localStorage:", error)
    }
  }, [isEnabled])

  return [isEnabled, setIsEnabled] as const
}
