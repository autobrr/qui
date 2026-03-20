/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"

export function usePersistedCrossSeedBlocklist(instanceId: number, defaultValue: boolean = false) {
  const readStoredPreference = useCallback(() => {
    if (instanceId <= 0) return undefined

    try {
      const storageKey = `qui-cross-seed-blocklist-${instanceId}`
      const existingPreference = localStorage.getItem(storageKey)
      if (existingPreference) {
        const parsedPreference = JSON.parse(existingPreference)
        if (typeof parsedPreference === "boolean") {
          return parsedPreference
        }
      }
    } catch (error) {
      console.error("Failed to read cross-seed blocklist preference from localStorage:", error)
    }

    return undefined
  }, [instanceId])

  const [blockCrossSeeds, setBlockCrossSeeds] = useState<boolean>(() => readStoredPreference() ?? defaultValue)

  useEffect(() => {
    const storedPreference = readStoredPreference()
    if (typeof storedPreference === "boolean") {
      setBlockCrossSeeds(storedPreference)
      return
    }

    setBlockCrossSeeds(defaultValue)
  }, [defaultValue, readStoredPreference])

  useEffect(() => {
    if (instanceId <= 0) return

    try {
      const storageKey = `qui-cross-seed-blocklist-${instanceId}`
      localStorage.setItem(storageKey, JSON.stringify(blockCrossSeeds))
    } catch (error) {
      console.error("Failed to save cross-seed blocklist preference to localStorage:", error)
    }
  }, [blockCrossSeeds, instanceId])

  return {
    blockCrossSeeds,
    setBlockCrossSeeds,
  } as const
}
