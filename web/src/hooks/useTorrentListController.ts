/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { buildVisibleRows, COLLAPSED_COPY_GROUPS, copyGroupExpansionReducer } from "@/lib/copy-groups"
import { EMPTY_LOAD_MORE_WINDOW, LOAD_MORE_STEP, loadMoreWindowReducer } from "@/lib/load-more-window"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import type { TorrentFilters } from "@/types"
import { useCallback, useMemo, useReducer, useState } from "react"

export { TORRENT_STREAM_POLL_INTERVAL_SECONDS } from "@/hooks/useTorrentsList"

export interface UseTorrentListControllerParams {
  instanceId: number
  instanceIds?: number[]
  filters: TorrentFilters
  columnFiltersExpr?: string | null
  search: string
  sort: string
  order: "asc" | "desc"
  groupCopies?: boolean
}

export function useTorrentListController({
  instanceId,
  instanceIds,
  filters,
  columnFiltersExpr = null,
  search,
  sort,
  order,
  groupCopies = false,
}: UseTorrentListControllerParams) {
  const { isHiddenDelayed, isVisible } = useDelayedVisibility(3000)

  const listFilters = useMemo(() => {
    const expr = isCrossSeedExpr(filters.expr) ? filters.expr : combineFilterExpr(columnFiltersExpr, filters.expr)
    return { ...filters, expr: expr || undefined }
  }, [filters, columnFiltersExpr])

  const requestGroups = groupCopies && !isCrossSeedExpr(filters.expr)

  const { loadMore: loadNextPage, ...list } = useTorrentsList(instanceId, {
    pollingEnabled: isHiddenDelayed || isVisible,
    instanceIds,
    search,
    filters: listFilters,
    sort,
    order,
    groupCopies: requestGroups,
  })
  const { torrents, hasLoadedAll, isLoadingMore } = list

  const [loadMoreWindow, dispatch] = useReducer(loadMoreWindowReducer, EMPTY_LOAD_MORE_WINDOW)
  const [groupExpansion, dispatchExpansion] = useReducer(copyGroupExpansionReducer, COLLAPSED_COPY_GROUPS)

  const listKey = useMemo(
    () => JSON.stringify([instanceId, instanceIds, filters, columnFiltersExpr, search, sort, order, requestGroups]),
    [instanceId, instanceIds, filters, columnFiltersExpr, search, sort, order, requestGroups]
  )
  const selectionScopeKey = useMemo(
    () => JSON.stringify([instanceId, instanceIds, filters, columnFiltersExpr, search]),
    [instanceId, instanceIds, filters, columnFiltersExpr, search]
  )
  const [windowListKey, setWindowListKey] = useState(listKey)
  const [expansionScopeKey, setExpansionScopeKey] = useState(selectionScopeKey)

  const visibleRows = useMemo(() => buildVisibleRows(torrents, groupExpansion), [torrents, groupExpansion])

  if (windowListKey !== listKey) {
    setWindowListKey(listKey)
    dispatch({ type: "queryChanged" })
  }
  if (expansionScopeKey !== selectionScopeKey) {
    setExpansionScopeKey(selectionScopeKey)
    dispatchExpansion({ type: "scopeChanged" })
  }
  if (loadMoreWindow.listLength !== visibleRows.length) {
    dispatch({ type: "dataArrived", length: visibleRows.length })
  }
  if (!isLoadingMore && loadMoreWindow.pageRequested && loadMoreWindow.listLength > loadMoreWindow.loadedRows) {
    dispatch({ type: "pageLoaded" })
  }

  const reportNearEnd = () => {
    if (loadMoreWindow.loadedRows + LOAD_MORE_STEP >= loadMoreWindow.listLength) {
      loadNextPage()
    }
    const event = { type: "nearEnd", hasMorePages: !hasLoadedAll } as const
    if (loadMoreWindowReducer(loadMoreWindow, event) !== loadMoreWindow) {
      dispatch(event)
    }
  }

  const toggleGroup = useCallback((id: string) => dispatchExpansion({ type: "toggleGroup", id }), [])

  return {
    ...list,
    visibleRows,
    loadedRows: Math.min(loadMoreWindow.loadedRows, visibleRows.length),
    hasMoreRows: loadMoreWindow.loadedRows < loadMoreWindow.listLength || !hasLoadedAll,
    reportNearEnd,
    listKey,
    groupExpansion,
    toggleGroup,
  }
}
