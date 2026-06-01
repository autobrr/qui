/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDebounce } from "@/hooks/useDebounce"
import { columnFiltersToExpr, type ColumnFilter } from "@/lib/column-filter-utils"
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
  globalFilter: string
  setGlobalFilter: Dispatch<SetStateAction<string>>
  effectiveSearch: string
  columnFiltersExpr: string | null
  combinedFiltersExpr: string | null | undefined
  isDoingCrossSeedFiltering: boolean | undefined
  lastUserAction: UserAction | null
  setLastUserAction: Dispatch<SetStateAction<UserAction | null>>
}

/**
 * Owns the table's search + filter-expression derivation: the debounced/route
 * search inputs, the column-filter-to-expr conversion, the cross-seed detection,
 * and the combined backend expression.
 *
 * The `combinedFiltersExpr` cross-seed early-return is load-bearing (regression
 * #1925): in cross-seed mode the column filters are applied client-side by
 * TanStack Table, so they must NOT be folded into the backend expression. This
 * is deliberately DIFFERENT from `selectAllFilters` (bulk-action targeting),
 * which always combines column filters with `filters.expr`. Keep them divergent.
 */
export function useTorrentTableFilterExpr({
  filters,
  instanceId,
  columnFilters,
}: UseTorrentTableFilterExprParams): TorrentTableFilterExpr {
  const [globalFilter, setGlobalFilter] = useState("")
  // Track user-initiated actions to differentiate from automatic data updates
  const [lastUserAction, setLastUserAction] = useState<UserAction | null>(null)
  const previousFiltersRef = useRef(filters)
  const previousInstanceIdRef = useRef(instanceId)

  // Debounce search to prevent excessive filtering (200ms delay for faster response)
  const debouncedSearch = useDebounce(globalFilter, 200)
  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const rawRouteSearch = typeof routeSearch?.q === "string" ? routeSearch.q : ""
  const searchFromRoute = rawRouteSearch.trim()

  // Use route search if present, otherwise fall back to the local debounced search
  const effectiveSearch = (searchFromRoute || debouncedSearch).trim()

  // Seed with the initial effectiveSearch so a route-derived search present on
  // mount (e.g. loading a URL with ?q=) isn't mistaken for a user-initiated
  // search action and doesn't emit a spurious {type: "search"}.
  const previousSearchRef = useRef(effectiveSearch)

  // Keep local input state in sync with route query so internal effects remain consistent
  useEffect(() => {
    if (searchFromRoute !== globalFilter) {
      setGlobalFilter(searchFromRoute)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchFromRoute])

  // Convert column filters to expr format for backend
  const columnFiltersExpr = useMemo(() => columnFiltersToExpr(columnFilters), [columnFilters])

  // Detect if this is cross-seed filtering (same logic as in useTorrentsList)
  const isDoingCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes("Hash ==") && filters?.expr?.includes("||")
  }, [filters?.expr])

  // Combine column filters with any existing filter expression
  // For cross-seed filtering, we'll apply column filters client-side only
  const combinedFiltersExpr = useMemo(() => {
    const columnExpr = columnFiltersExpr
    const filterExpr = filters?.expr

    // If we're doing cross-seed filtering, don't send column filters to backend
    // They will be applied client-side by TanStack Table (along with sorting)
    if (isDoingCrossSeedFiltering) {
      return filterExpr // Only use the cross-seed expression for backend
    }

    // For regular filtering, combine column filters with existing filters
    if (columnExpr && filterExpr) {
      const combined = `(${columnExpr}) && (${filterExpr})`
      return combined
    }
    return columnExpr || filterExpr
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
    globalFilter,
    setGlobalFilter,
    effectiveSearch,
    columnFiltersExpr,
    combinedFiltersExpr,
    isDoingCrossSeedFiltering,
    lastUserAction,
    setLastUserAction,
  }
}
