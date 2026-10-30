/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { columnFiltersToExpr, type ColumnFilter } from "@/lib/column-filter-utils"
import { isCrossSeedExpr } from "@/lib/torrent-filters"
import type { TorrentFilters } from "@/types"
import { useSearch } from "@tanstack/react-router"
import { useMemo } from "react"

export interface UseTorrentTableFilterExprParams {
  filters?: TorrentFilters
  columnFilters: ColumnFilter[]
}

export interface TorrentTableFilterExpr {
  effectiveSearch: string
  columnFiltersExpr: string | null
  isDoingCrossSeedFiltering: boolean
}

/**
 * Owns the table's search + filter-expression derivation: the route search
 * (`?q=`, already debounced by the header input), the column-filter-to-expr
 * conversion, and the cross-seed detection. useTorrentListController joins the
 * column filters into the list request.
 */
export function useTorrentTableFilterExpr({
  filters,
  columnFilters,
}: UseTorrentTableFilterExprParams): TorrentTableFilterExpr {
  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const rawRouteSearch = typeof routeSearch?.q === "string" ? routeSearch.q : ""
  const effectiveSearch = rawRouteSearch.trim()

  // Convert column filters to expr format for backend
  const columnFiltersExpr = useMemo(() => columnFiltersToExpr(columnFilters), [columnFilters])

  const isDoingCrossSeedFiltering = isCrossSeedExpr(filters?.expr)

  return {
    effectiveSearch,
    columnFiltersExpr,
    isDoingCrossSeedFiltering,
  }
}
