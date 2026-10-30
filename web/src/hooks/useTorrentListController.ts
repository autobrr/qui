/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { EMPTY_LOAD_MORE_WINDOW, loadMoreWindowReducer } from "@/lib/load-more-window"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import type { ActionTarget } from "@/lib/torrent-selection"
import { anyTorrentHasTag } from "@/lib/torrent-utils"
import type { TorrentFilters } from "@/types"
import { useMemo, useReducer, useState } from "react"

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
}

/**
 * Loads the torrent list for the desktop table and the mobile cards, and holds
 * the load-more window. A Layout shows `loadedRows` rows and calls
 * `reportNearEnd` when the user scrolls near the end of them. A change of the
 * instance, the filters, the search or the sort starts the window again at the
 * first rows and changes `listKey`; a data update keeps the window.
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

  const { loadMore: loadNextPage, ...list } = useTorrentsList(instanceId, {
    pollingEnabled: isHiddenDelayed || isVisible,
    instanceIds,
    search,
    filters: listFilters,
    sort,
    order,
  })
  const { torrents, hasLoadedAll, isLoadingMore } = list

  const [loadMoreWindow, dispatch] = useReducer(loadMoreWindowReducer, EMPTY_LOAD_MORE_WINDOW)

  // The inputs only change on a user action, so a new key is a user change.
  const listKey = useMemo(
    () => JSON.stringify([instanceId, instanceIds, filters, columnFiltersExpr, search, sort, order]),
    [instanceId, instanceIds, filters, columnFiltersExpr, search, sort, order]
  )
  const [windowListKey, setWindowListKey] = useState(listKey)

  // Render-time adjustments: React renders again before it commits, so no
  // committed render shows a window from the previous list.
  if (windowListKey !== listKey) {
    setWindowListKey(listKey)
    dispatch({ type: "queryChanged" })
  }
  if (loadMoreWindow.listLength !== torrents.length) {
    dispatch({ type: "dataArrived", length: torrents.length })
  }
  // The list hook appends the rows of a page one render after isLoadingMore
  // clears, so wait until the rows are in memory.
  if (!isLoadingMore && loadMoreWindow.pageRequested && loadMoreWindow.listLength > loadMoreWindow.loadedRows) {
    dispatch({ type: "pageLoaded" })
  }

  const canLoadPage = !hasLoadedAll && !isLoadingMore
  const reportNearEnd = () => {
    if (loadMoreWindow.loadedRows >= loadMoreWindow.listLength && canLoadPage) {
      loadNextPage()
    }
    dispatch({ type: "nearEnd", canLoadPage })
  }

  return {
    ...list,
    // The rest of this render pass still runs with the old window, so clamp it.
    loadedRows: Math.min(loadMoreWindow.loadedRows, torrents.length),
    hasMoreRows: loadMoreWindow.loadedRows < loadMoreWindow.listLength || !hasLoadedAll,
    reportNearEnd,
    listKey,
  }
}

/**
 * The cross-seed state of the Action target: the warning (the cross-seeds that
 * a delete affects), whether the target carries the cross-seed tag, whether a
 * delete blocks the cross-seeds, and the blocklist action. It is a separate
 * hook because each Layout builds the Action target from the controller's rows.
 */
export function useTorrentListCrossSeed({
  instanceId,
  actionTarget,
  blockCrossSeeds,
}: {
  instanceId: number
  actionTarget: ActionTarget
  blockCrossSeeds: boolean
}) {
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    torrents: actionTarget.previewTorrents,
    selectAll: actionTarget.kind === "selectAll" ? actionTarget.selectAll : undefined,
  })
  const hasCrossSeedTag = useMemo(
    () => anyTorrentHasTag(actionTarget.previewTorrents, "cross-seed") || anyTorrentHasTag(crossSeedWarning.affectedTorrents, "cross-seed"),
    [actionTarget.previewTorrents, crossSeedWarning.affectedTorrents]
  )
  const shouldBlockCrossSeeds = hasCrossSeedTag && blockCrossSeeds
  const { blockCrossSeedHashes } = useCrossSeedBlocklistActions(instanceId)

  return { crossSeedWarning, hasCrossSeedTag, shouldBlockCrossSeeds, blockCrossSeedHashes }
}
