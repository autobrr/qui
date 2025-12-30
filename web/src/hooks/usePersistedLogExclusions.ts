/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

const STORAGE_KEY = "qui-log-exclusions"

function isBrowser() {
  return typeof window !== "undefined" && typeof window.localStorage !== "undefined"
}

/**
 * Hook for persisting muted log message patterns to localStorage.
 * Returns [exclusions, setExclusions] where exclusions is an array of message strings to hide.
 */
export function usePersistedLogExclusions() {
  const [exclusions, setExclusions] = useState<string[]>(() => {
    if (!isBrowser()) {
      return []
    }
    try {
      const stored = window.localStorage.getItem(STORAGE_KEY)
      if (stored) {
        const parsed: unknown = JSON.parse(stored)
        if (Array.isArray(parsed) && parsed.every(item => typeof item === "string")) {
          return parsed as string[]
        }
      }
    } catch (error) {
      console.error("Failed to load log exclusions from localStorage:", error)
    }
    return []
  })

  // Persist to localStorage when exclusions change
  useEffect(() => {
    if (!isBrowser()) {
      return
    }
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(exclusions))
    } catch (error) {
      console.error("Failed to save log exclusions to localStorage:", error)
    }
  }, [exclusions])

  return [exclusions, setExclusions] as const
}
