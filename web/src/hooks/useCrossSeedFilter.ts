/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useRef, useState } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { Torrent, TorrentFilters } from "@/types"

interface UseCrossSeedFilterOptions {
  instanceId: number
  onFilterChange?: (filters: TorrentFilters) => void
}

export function useCrossSeedFilter({ instanceId, onFilterChange }: UseCrossSeedFilterOptions) {
  const [isFilteringCrossSeeds, setIsFilteringCrossSeeds] = useState(false)
  const isFilteringRef = useRef(false)

  const filterCrossSeeds = useCallback(async (torrents: Torrent[]) => {
    if (!onFilterChange) {
      toast.error("Filtering is unavailable in this view")
      return
    }

    if (isFilteringRef.current) {
      return
    }

    if (torrents.length !== 1) {
      toast.info("Cross-seed filtering only works with a single selected torrent")
      return
    }

    const selectedTorrent = torrents[0]
    isFilteringRef.current = true
    setIsFilteringCrossSeeds(true)
    toast.info("Identifying cross-seeded torrents...")

    try {
      // Use backend API for proper release matching (rls library)
      // This searches all instances in one call
      const matches = await api.getLocalCrossSeedMatches(instanceId, selectedTorrent.hash)

      if (matches.length === 0) {
        toast.info("No cross-seeded torrents found")
        return
      }

      const hashConditions = matches.map(match => `Hash == "${match.hash}"`)
      hashConditions.push(`Hash == "${selectedTorrent.hash}"`)
      const uniqueConditions = [...new Set(hashConditions)]

      const newFilters: TorrentFilters = {
        status: [],
        excludeStatus: [],
        categories: [],
        excludeCategories: [],
        tags: [],
        excludeTags: [],
        trackers: [],
        excludeTrackers: [],
        expr: uniqueConditions.join(" || "),
      }

      onFilterChange(newFilters)
      toast.success(`Found ${matches.length} cross-seeded torrents (showing ${uniqueConditions.length} total)`)
    } catch (error) {
      console.error("Failed to identify cross-seeded torrents:", error)
      toast.error("Failed to identify cross-seeded torrents")
    } finally {
      isFilteringRef.current = false
      setIsFilteringCrossSeeds(false)
    }
  }, [instanceId, onFilterChange])

  return { isFilteringCrossSeeds, filterCrossSeeds }
}
