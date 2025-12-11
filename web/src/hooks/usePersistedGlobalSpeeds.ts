/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

function isBrowser() {
  return typeof window !== "undefined" && typeof window.localStorage !== "undefined"
}

const GLOBAL_SPEEDS_STORAGE_KEY = "qui-global-speeds"

export function usePersistedGlobalSpeeds(): [boolean, (value: boolean) => void] {
  const [showGlobalSpeeds, setShowGlobalSpeedsState] = useState(() => {
    if (!isBrowser()) {
      return false
    }
    const stored = localStorage.getItem(GLOBAL_SPEEDS_STORAGE_KEY)
    return stored === "true"
  })

  // Listen for storage changes to sync global speeds
  useEffect(() => {
    if (!isBrowser()) {
      return
    }

    const handleStorageChange = () => {
      const stored = localStorage.getItem(GLOBAL_SPEEDS_STORAGE_KEY)
      setShowGlobalSpeedsState(stored === "true")
    }

    // Listen for both storage events (cross-tab) and custom events (same-tab)
    window.addEventListener("storage", handleStorageChange)
    window.addEventListener("global-speeds-changed", handleStorageChange)

    return () => {
      window.removeEventListener("storage", handleStorageChange)
      window.removeEventListener("global-speeds-changed", handleStorageChange)
    }
  }, [])

  const setShowGlobalSpeeds = (value: boolean) => {
    setShowGlobalSpeedsState(value)
    if (!isBrowser()) {
      return
    }
    localStorage.setItem(GLOBAL_SPEEDS_STORAGE_KEY, String(value))
    // Dispatch custom event for same-tab updates
    window.dispatchEvent(new Event("global-speeds-changed"))
  }

  return [showGlobalSpeeds, setShowGlobalSpeeds]
}
