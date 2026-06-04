/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import type { TorrentFilters } from "@/types"
import type { SortingState } from "@tanstack/react-table"
import type { Virtualizer } from "@tanstack/react-virtual"
import { useCallback, useEffect, useLayoutEffect, useState, type Dispatch, type SetStateAction } from "react"

export type FilterLifecycleState = "idle" | "clearing-all" | "clearing-columns-only" | "cleared"

export interface UseFilterLifecycleParams {
  virtualizer: Virtualizer<HTMLDivElement, Element>
  sortedTorrentsLength: number
  onFilterChange?: (filters: TorrentFilters) => void
  setColumnFilters: Dispatch<SetStateAction<ColumnFilter[]>>
  setSorting: Dispatch<SetStateAction<SortingState>>
  /** Shared with useTorrentTableVirtualization — the same loaded-rows window. */
  setLoadedRows: Dispatch<SetStateAction<number>>
  isCrossSeedFiltering?: boolean
  columnFiltersLength: number
  visibleRowCount: number
  loadedRows: number
}

export interface FilterLifecycle {
  filterLifecycleState: FilterLifecycleState
  clearFiltersAtomically: (mode?: "all" | "columns-only") => void
}

/**
 * The filter-clearing state machine. `clearFiltersAtomically` arms a transition;
 * a single pre-paint useLayoutEffect then performs the clear as one atomic
 * transaction (column filters + sorting + virtualizer reset + loaded-rows reset,
 * plus the parent onFilterChange when clearing all) before settling back to idle.
 *
 * The effect dep arrays are kept byte-for-byte identical to the pre-extraction
 * code: the various state setters are stable references threaded in as params, so
 * they are intentionally omitted (the eslint-disable below documents that). Do not
 * "complete" these dep arrays — it changes nothing at runtime and only adds noise.
 */
export function useFilterLifecycle({
  virtualizer,
  sortedTorrentsLength,
  onFilterChange,
  setColumnFilters,
  setSorting,
  setLoadedRows,
  isCrossSeedFiltering,
  columnFiltersLength,
  visibleRowCount,
  loadedRows,
}: UseFilterLifecycleParams): FilterLifecycle {
  const [filterLifecycleState, setFilterLifecycleState] = useState<FilterLifecycleState>("idle")

  // Atomic filter clearing callback
  const clearFiltersAtomically = useCallback((mode: "all" | "columns-only" = "all") => {
    setFilterLifecycleState(mode === "all" ? "clearing-all" : "clearing-columns-only")
  }, [])

  // Fix virtualization when column filters are cleared in cross-seed mode
  // Only run when lifecycle is idle to avoid racing with filter lifecycle handler
  useEffect(() => {
    if (filterLifecycleState === "idle" && isCrossSeedFiltering && columnFiltersLength === 0) {
      // Reset loadedRows to ensure all rows are visible when filters are cleared
      const targetRows = Math.min(100, sortedTorrentsLength)
      // Use functional update to ensure idempotent, non-racing updates
      setLoadedRows(prev => Math.max(prev, targetRows))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterLifecycleState, isCrossSeedFiltering, columnFiltersLength, sortedTorrentsLength])

  // Also keep loadedRows in sync with actual data to prevent status display issues
  useEffect(() => {
    if (filterLifecycleState === "idle" && loadedRows > visibleRowCount && visibleRowCount > 0) {
      setLoadedRows(visibleRowCount)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadedRows, visibleRowCount, filterLifecycleState])

  // Filter lifecycle state machine
  useLayoutEffect(() => {
    if (filterLifecycleState === "clearing-all" || filterLifecycleState === "clearing-columns-only") {

      // Perform clearing operations atomically
      setColumnFilters([])
      setSorting([])
      virtualizer.scrollToOffset(0)
      virtualizer.measure()

      // Reset loadedRows to a reasonable initial value
      const newLoadedRows = Math.min(100, sortedTorrentsLength)
      setLoadedRows(newLoadedRows)

      // Only clear parent filters if clearing all (not just columns)
      if (filterLifecycleState === "clearing-all") {
        const emptyFilters: TorrentFilters = {
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
        }
        onFilterChange?.(emptyFilters)
      }

      // Transition to cleared state
      setFilterLifecycleState("cleared")
    } else if (filterLifecycleState === "cleared") {
      // Reset to idle state after clearing is complete
      setFilterLifecycleState("idle")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterLifecycleState, virtualizer, onFilterChange, setLoadedRows, sortedTorrentsLength])

  return { filterLifecycleState, clearFiltersAtomically }
}
