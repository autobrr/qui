/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import { useEffect, useState } from "react"

export function usePersistedColumnFilters(instanceId: number) {
  const storageKey = `qui-column-filters-${instanceId}`

  // Initialize state with persisted values immediately
  const [columnFilters, setColumnFilters] = useState<ColumnFilter[]>(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      return stored ? JSON.parse(stored) : []
    } catch {
      return []
    }
  })

  // Load filters when instanceId changes
  useEffect(() => {
    try {
      const stored = localStorage.getItem(storageKey)
      setColumnFilters(stored ? JSON.parse(stored) : [])
    } catch {
      setColumnFilters([])
    }
  }, [instanceId, storageKey])

  // Save filters when they change
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(columnFilters))
    } catch (error) {
      console.error("Failed to save column filters:", error)
    }
  }, [columnFilters, storageKey])

  return [columnFilters, setColumnFilters] as const
}