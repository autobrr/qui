/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

export interface DateTimePreferences {
  timezone: string
  timeFormat: "12h" | "24h"
  dateFormat: "iso" | "us" | "eu" | "relative"
}

const DEFAULT_PREFERENCES: DateTimePreferences = {
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  timeFormat: "24h",
  dateFormat: "iso"
}

export function usePersistedDateTimePreferences() {
  const storageKey = "qui-datetime-preferences"

  // Initialize state from localStorage or default values
  const [preferences, setPreferencesState] = useState<DateTimePreferences>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        return {
          ...DEFAULT_PREFERENCES,
          ...parsed
        }
      }
    } catch (error) {
      console.error("Failed to load date/time preferences from localStorage:", error)
    }

    return DEFAULT_PREFERENCES
  })

  // Persist to localStorage whenever preferences change
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(preferences))
    } catch (error) {
      console.error("Failed to save date/time preferences to localStorage:", error)
    }
  }, [preferences])

  // Listen for cross-component updates via CustomEvent within the same tab
  useEffect(() => {
    const handleEvent = (e: Event) => {
      const custom = e as CustomEvent<{ preferences: DateTimePreferences }>
      if (custom.detail?.preferences) {
        setPreferencesState(custom.detail.preferences)
      }
    }

    window.addEventListener(storageKey, handleEvent as EventListener)
    return () => window.removeEventListener(storageKey, handleEvent as EventListener)
  }, [])

  // Update preferences function
  const setPreferences = (newPreferences: Partial<DateTimePreferences>) => {
    setPreferencesState((prev) => {
      const updated = { ...prev, ...newPreferences }
      
      try {
        localStorage.setItem(storageKey, JSON.stringify(updated))
      } catch (error) {
        console.error("Failed to save date/time preferences to localStorage:", error)
      }
      
      // Notify other components via CustomEvent
      const evt = new CustomEvent(storageKey, { detail: { preferences: updated } })
      window.dispatchEvent(evt)
      
      return updated
    })
  }

  return {
    preferences,
    setPreferences,
    resetToDefaults: () => setPreferences(DEFAULT_PREFERENCES)
  } as const
}