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
  /** The column filters as one expression. The mobile cards have none. */
  columnFiltersExpr?: string | null
  search: string
  sort: string
  order: "asc" | "desc"
  /** Ask for Copy groups. Filter Cross-Seeds never asks. */
  groupCopies?: boolean
}

/**
 * Loads the torrent list for the desktop table and the mobile cards, and holds
 * the load-more window. A Layout shows `loadedRows` of `visibleRows` and calls
 * `reportNearEnd` when the user scrolls near the end of them. A change of the
 * instance, the filters, the search, the sort or the grouping starts the window
 * again at the first rows and changes `listKey`; a data update keeps the window.
 *
 * `torrents` holds every loaded row. `visibleRows` shows each Copy group as its
 * top row, with its other members under it while the group is open; on a flat
 * list it is `torrents` itself. The open groups reset when the selection scope
 * changes, and stay on a sort change.
 *
 * Polling stops while the tab is hidden, the list refetches when the window
 * gets focus again, and the list hook closes the live stream after the tab is
 * hidden for STREAM_HIDDEN_PAUSE_DELAY_MS.
 */
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
  // React Query stops the polling while the tab is hidden. A tab that comes
  // back within 3 s skips the focus refetch; a later return refetches.
  const { isHiddenDelayed, isVisible } = useDelayedVisibility(3000)

  // In cross-seed mode the column filters apply client-side, so the request
  // gets the hash expression alone. The Action target always joins them (#1925).
  const listFilters = useMemo(() => {
    const expr = isCrossSeedExpr(filters.expr) ? filters.expr : combineFilterExpr(columnFiltersExpr, filters.expr)
    return { ...filters, expr: expr || undefined }
  }, [filters, columnFiltersExpr])

  // The list hook does not ask for groups in Filter Cross-Seeds, so the switch changes nothing there.
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

  // The inputs only change on a user action, so a new key is a user change.
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

  // Render-time adjustments: React renders again before it commits, so no
  // committed render shows a window from the previous list.
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
  // The list hook appends the rows of a page one render after isLoadingMore
  // clears, so wait until the rows are in memory.
  if (!isLoadingMore && loadMoreWindow.pageRequested && loadMoreWindow.listLength > loadMoreWindow.loadedRows) {
    dispatch({ type: "pageLoaded" })
  }

  // The table reports on each scroll frame near the end, so this must be cheap.
  const reportNearEnd = () => {
    // Ask for the next server page when the window shows the last step in
    // memory, so its rows arrive before a fast scroll reaches them.
    // loadNextPage ignores a call while a page loads.
    if (loadMoreWindow.loadedRows + LOAD_MORE_STEP >= loadMoreWindow.listLength) {
      loadNextPage()
    }
    const event = { type: "nearEnd", hasMorePages: !hasLoadedAll } as const
    // A dispatch that changes nothing still renders the Layout again.
    if (loadMoreWindowReducer(loadMoreWindow, event) !== loadMoreWindow) {
      dispatch(event)
    }
  }

  const toggleGroup = useCallback((id: string) => dispatchExpansion({ type: "toggleGroup", id }), [])

  return {
    ...list,
    visibleRows,
    // The rest of this render pass still runs with the old window, so clamp it.
    loadedRows: Math.min(loadMoreWindow.loadedRows, visibleRows.length),
    hasMoreRows: loadMoreWindow.loadedRows < loadMoreWindow.listLength || !hasLoadedAll,
    reportNearEnd,
    listKey,
    groupExpansion,
    toggleGroup,
  }
}
