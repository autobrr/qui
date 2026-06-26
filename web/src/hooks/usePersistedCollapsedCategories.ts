/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"

function isBrowser() {
  return typeof window !== "undefined" && typeof window.localStorage !== "undefined"
}

export function usePersistedCollapsedCategories(instanceId: number) {
  const storageKey = `qui-collapsed-categories-${instanceId}`

  // Initialize state from localStorage
  const [collapsedCategories, setCollapsedCategories] = useState<Set<string>>(() => {
    if (!isBrowser()) {
      return new Set()
    }
    try {
      const stored = window.localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        if (Array.isArray(parsed) && parsed.every(item => typeof item === "string")) {
          return new Set(parsed)
        }
      }
    } catch (error) {
      console.error("Failed to load collapsed categories from localStorage:", error)
    }
    return new Set()
  })

  // Reload when instanceId changes
  useEffect(() => {
    if (!isBrowser()) {
      setCollapsedCategories(new Set())
      return
    }
    try {
      const stored = window.localStorage.getItem(storageKey)
      if (stored) {
        const parsed = JSON.parse(stored)
        if (Array.isArray(parsed) && parsed.every(item => typeof item === "string")) {
          setCollapsedCategories(new Set(parsed))
          return
        }
      }
    } catch (error) {
      console.error("Failed to load collapsed categories from localStorage:", error)
    }
    setCollapsedCategories(new Set())
  }, [storageKey])

  useEffect(() => {
    if (!isBrowser()) {
      return
    }
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(Array.from(collapsedCategories)))
    } catch (error) {
      console.error("Failed to save collapsed categories to localStorage:", error)
    }
  }, [collapsedCategories, storageKey])

  return [collapsedCategories, setCollapsedCategories] as const
}
