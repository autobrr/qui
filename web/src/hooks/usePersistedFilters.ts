/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState } from "react"
import type { TorrentFilters } from "@/types"

// Safe localStorage wrapper that returns fallback on error
function safeGetItem(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch (error) {
    console.error(`Failed to read from localStorage key "${key}":`, error)
    return null
  }
}

function safeSetItem(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch (error) {
    console.error(`Failed to write to localStorage key "${key}":`, error)
  }
}

export function usePersistedFilters(instanceId: number) {
  // Initialize state with persisted values immediately
  const [filters, setFilters] = useState<TorrentFilters>(() => {
    const global = JSON.parse(safeGetItem("qui-filters-global") || "{}")
    const instance = JSON.parse(safeGetItem(`qui-filters-${instanceId}`) || "{}")

    return {
      status: global.status || [],
      excludeStatus: global.excludeStatus || [],
      categories: instance.categories || [],
      excludeCategories: instance.excludeCategories || [],
      tags: instance.tags || [],
      excludeTags: instance.excludeTags || [],
      trackers: instance.trackers || [],
      excludeTrackers: instance.excludeTrackers || [],
      expr: instance.expr || "",
    }
  })

  // Load filters when instanceId changes
  useEffect(() => {
    const global = JSON.parse(safeGetItem("qui-filters-global") || "{}")
    const instance = JSON.parse(safeGetItem(`qui-filters-${instanceId}`) || "{}")

    setFilters({
      status: global.status || [],
      excludeStatus: global.excludeStatus || [],
      categories: instance.categories || [],
      excludeCategories: instance.excludeCategories || [],
      tags: instance.tags || [],
      excludeTags: instance.excludeTags || [],
      trackers: instance.trackers || [],
      excludeTrackers: instance.excludeTrackers || [],
      expr: instance.expr || "",
    })
  }, [instanceId])

  // Save filters when they change
  useEffect(() => {
    safeSetItem("qui-filters-global", JSON.stringify({
      status: filters.status,
      excludeStatus: filters.excludeStatus,
    }))
    safeSetItem(`qui-filters-${instanceId}`, JSON.stringify({
      categories: filters.categories,
      excludeCategories: filters.excludeCategories,
      tags: filters.tags,
      excludeTags: filters.excludeTags,
      trackers: filters.trackers,
      excludeTrackers: filters.excludeTrackers,
      expr: filters.expr,
    }))
  }, [filters, instanceId])

  return [filters, setFilters] as const
}
