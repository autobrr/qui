/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { columnFiltersToExpr, type ColumnFilter } from "@/lib/column-filter-utils"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import type { TorrentFilters } from "@/types"
import { useSearch } from "@tanstack/react-router"
import { type Dispatch, type SetStateAction, useEffect, useMemo, useRef, useState } from "react"

export interface UseTorrentTableFilterExprParams {
  filters?: TorrentFilters
  instanceId: number
  columnFilters: ColumnFilter[]
}

export interface UserAction {
  type: string
  timestamp: number
}

export interface TorrentTableFilterExpr {
  effectiveSearch: string
  columnFiltersExpr: string | null
  combinedFiltersExpr: string | null | undefined
  isDoingCrossSeedFiltering: boolean
  lastUserAction: UserAction | null
  setLastUserAction: Dispatch<SetStateAction<UserAction | null>>
}

/**
 * Owns the table's search + filter-expression derivation: the route search
 * (`?q=`, already debounced by the header input), the column-filter-to-expr
 * conversion, the cross-seed detection, and the combined backend expression.
 *
 * The cross-seed early return is the one deliberate difference from
 * `selectAllFilters`, which always combines (#1925); both are pinned in their
 * hook tests.
 */
export function useTorrentTableFilterExpr({
  filters,
  instanceId,
  columnFilters,
}: UseTorrentTableFilterExprParams): TorrentTableFilterExpr {
  // Track user-initiated actions to differentiate from automatic data updates
  const [lastUserAction, setLastUserAction] = useState<UserAction | null>(null)
  const previousFiltersRef = useRef(filters)
  const previousInstanceIdRef = useRef(instanceId)

  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const rawRouteSearch = typeof routeSearch?.q === "string" ? routeSearch.q : ""
  const effectiveSearch = rawRouteSearch.trim()

  // Seed with the initial effectiveSearch so a route-derived search present on
  // mount (e.g. loading a URL with ?q=) isn't mistaken for a user-initiated
  // search action and doesn't emit a spurious {type: "search"}.
  const previousSearchRef = useRef(effectiveSearch)

  // Convert column filters to expr format for backend
  const columnFiltersExpr = useMemo(() => columnFiltersToExpr(columnFilters), [columnFilters])

  const isDoingCrossSeedFiltering = isCrossSeedExpr(filters?.expr)

  // In cross-seed mode the column filters are applied client-side by TanStack
  // Table, so the backend gets the hash expression alone.
  const combinedFiltersExpr = useMemo(() => {
    if (isDoingCrossSeedFiltering) {
      return filters?.expr
    }
    return combineFilterExpr(columnFiltersExpr, filters?.expr)
  }, [columnFiltersExpr, filters?.expr, isDoingCrossSeedFiltering])

  // Detect user-initiated changes
  useEffect(() => {
    const filtersChanged = JSON.stringify(previousFiltersRef.current) !== JSON.stringify(filters)
    const instanceChanged = previousInstanceIdRef.current !== instanceId
    const searchChanged = previousSearchRef.current !== effectiveSearch

    if (filtersChanged || instanceChanged || searchChanged) {
      setLastUserAction({
        type: instanceChanged ? "instance" : filtersChanged ? "filter" : "search",
        timestamp: Date.now(),
      })

      // Update refs
      previousFiltersRef.current = filters
      previousInstanceIdRef.current = instanceId
      previousSearchRef.current = effectiveSearch
    }
  }, [filters, instanceId, effectiveSearch])

  return {
    effectiveSearch,
    columnFiltersExpr,
    combinedFiltersExpr,
    isDoingCrossSeedFiltering,
    lastUserAction,
    setLastUserAction,
  }
}
