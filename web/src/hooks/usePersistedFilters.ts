/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"

interface Filters {
  status: string[]
  categories: string[]
  tags: string[]
  trackers: string[]
}

export function usePersistedFilters(instanceId: number) {
  // Initialize state with persisted values immediately
  const [filters, setFilters] = useState<Filters>(() => {
    const global = JSON.parse(localStorage.getItem("qui-filters-global") || "{}")
    const instance = JSON.parse(localStorage.getItem(`qui-filters-${instanceId}`) || "{}")
    
    return {
      status: global.status || [],
      categories: instance.categories || [],
      tags: instance.tags || [],
      trackers: instance.trackers || [],
    }
  })
  
  // Load filters when instanceId changes
  useEffect(() => {
    const global = JSON.parse(localStorage.getItem("qui-filters-global") || "{}")
    const instance = JSON.parse(localStorage.getItem(`qui-filters-${instanceId}`) || "{}")
    
    setFilters({
      status: global.status || [],
      categories: instance.categories || [],
      tags: instance.tags || [],
      trackers: instance.trackers || [],
    })
  }, [instanceId])
  
  // Save filters when they change
  useEffect(() => {
    localStorage.setItem("qui-filters-global", JSON.stringify({ status: filters.status }))
    localStorage.setItem(`qui-filters-${instanceId}`, JSON.stringify({
      categories: filters.categories,
      tags: filters.tags,
      trackers: filters.trackers,
    }))
  }, [filters, instanceId])
  
  return [filters, setFilters] as const
}