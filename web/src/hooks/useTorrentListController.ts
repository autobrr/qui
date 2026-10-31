/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import type { TorrentAction, useTorrentActions } from "@/hooks/useTorrentActions"
import type { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { EMPTY_LOAD_MORE_WINDOW, LOAD_MORE_STEP, loadMoreWindowReducer } from "@/lib/load-more-window"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import type { ActionTarget } from "@/lib/torrent-selection"
import { anyTorrentHasTag, getTorrentHashesWithTag } from "@/lib/torrent-utils"
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

  return {
    ...list,
    // The rest of this render pass still runs with the old window, so clamp it.
    loadedRows: Math.min(loadMoreWindow.loadedRows, torrents.length),
    hasMoreRows: loadMoreWindow.loadedRows < loadMoreWindow.listLength || !hasLoadedAll,
    reportNearEnd,
    listKey,
  }
}

type TorrentActions = ReturnType<typeof useTorrentActions>

export interface UseTorrentListActionsParams {
  instanceId: number
  actionTarget: ActionTarget
  /** The torrent actions of the Layout, with its dialog state. */
  actions: TorrentActions
  exportTorrents: ReturnType<typeof useTorrentExporter>["exportTorrents"]
  sort: string
  order: "asc" | "desc"
}

/**
 * The bulk actions of the Action target for both Layouts. Each wrapper passes
 * the value of a menu item or a dialog, with the Action target, to its handler
 * (ADR 0034). It also holds the cross-seed state of the target: the warning
 * (the cross-seeds that a delete affects) and whether the target carries the
 * cross-seed tag. A delete blocks the tagged torrents when the user asks for
 * it. It is a separate hook because each Layout builds the Action target from
 * the controller's rows.
 */
export function useTorrentListActions({
  instanceId,
  actionTarget,
  actions,
  exportTorrents,
  sort,
  order,
}: UseTorrentListActionsParams) {
  const {
    blockCrossSeeds,
    deleteCrossSeeds,
    handleAction,
    handleDelete,
    handleSetComment,
    handleUpdateTags,
    handleSetCategory,
    handleSetLocation,
    handleRenameTorrent,
    handleRenameFile,
    handleRenameFolder,
    handleRecheck,
    handleReannounce,
    handleTmmConfirm,
    handleSetShareLimit,
    handleSetSpeedLimits,
    prepareRecheckAction,
    prepareReannounceAction,
  } = actions

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

  // Rename acts on one torrent.
  const renameHash = actionTarget.previewTorrents[0]?.hash

  const runAction = useCallback((action: TorrentAction, options?: { enable?: boolean }) => {
    handleAction(action, actionTarget, options)
  }, [handleAction, actionTarget])

  const handleExportWrapper = useCallback(() => {
    void exportTorrents(actionTarget, sort, order)
  }, [exportTorrents, actionTarget, sort, order])

  const handleDeleteWrapper = useCallback(async () => {
    const crossSeedTargets = deleteCrossSeeds ? buildTorrentActionTargets(crossSeedWarning.affectedTorrents, instanceId) : []

    if (shouldBlockCrossSeeds) {
      const taggedHashes = getTorrentHashesWithTag(actionTarget.previewTorrents, "cross-seed")
      const crossSeedHashes = deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedWarning.affectedTorrents, "cross-seed") : []
      await blockCrossSeedHashes(
        [...taggedHashes, ...crossSeedHashes],
        [...buildTorrentActionTargets(actionTarget.previewTorrents, instanceId), ...crossSeedTargets]
      )
    }

    await handleDelete(actionTarget, crossSeedTargets)
  }, [actionTarget, blockCrossSeedHashes, crossSeedWarning.affectedTorrents, deleteCrossSeeds, handleDelete, instanceId, shouldBlockCrossSeeds])

  const handleSetCommentWrapper = useCallback((comment: string) => {
    void handleSetComment(comment, actionTarget)
  }, [handleSetComment, actionTarget])

  const handleTagsWrapper = useCallback((plan: Parameters<TorrentActions["handleUpdateTags"]>[0]) => {
    void handleUpdateTags(plan, actionTarget)
  }, [handleUpdateTags, actionTarget])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    void handleSetCategory(category, actionTarget)
  }, [handleSetCategory, actionTarget])

  const handleSetLocationWrapper = useCallback((location: string) => {
    void handleSetLocation(location, actionTarget)
  }, [handleSetLocation, actionTarget])

  const handleRenameTorrentWrapper = useCallback(async (name: string) => {
    if (!renameHash) return
    await handleRenameTorrent(renameHash, name)
  }, [handleRenameTorrent, renameHash])

  const handleRenameFileWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!renameHash || !oldPath || !newPath) return
    await handleRenameFile(renameHash, oldPath, newPath)
  }, [handleRenameFile, renameHash])

  const handleRenameFolderWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!renameHash || !oldPath || !newPath) return
    await handleRenameFolder(renameHash, oldPath, newPath)
  }, [handleRenameFolder, renameHash])

  const prepareRecheckWrapper = useCallback(() => prepareRecheckAction(actionTarget), [prepareRecheckAction, actionTarget])
  const prepareReannounceWrapper = useCallback(() => prepareReannounceAction(actionTarget), [prepareReannounceAction, actionTarget])
  const handleRecheckWrapper = useCallback(() => void handleRecheck(actionTarget), [handleRecheck, actionTarget])
  const handleReannounceWrapper = useCallback(() => void handleReannounce(actionTarget), [handleReannounce, actionTarget])
  const handleTmmConfirmWrapper = useCallback(() => handleTmmConfirm(actionTarget), [handleTmmConfirm, actionTarget])

  const handleSetShareLimitWrapper = useCallback((
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number,
    shareLimitAction?: string,
    shareLimitsMode?: string
  ) => {
    void handleSetShareLimit({ ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode }, actionTarget)
  }, [handleSetShareLimit, actionTarget])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    void handleSetSpeedLimits({ uploadLimit, downloadLimit }, actionTarget)
  }, [handleSetSpeedLimits, actionTarget])

  return {
    crossSeedWarning,
    hasCrossSeedTag,
    renameHash,
    runAction,
    handleExportWrapper,
    handleDeleteWrapper,
    handleSetCommentWrapper,
    handleTagsWrapper,
    handleSetCategoryWrapper,
    handleSetLocationWrapper,
    handleRenameTorrentWrapper,
    handleRenameFileWrapper,
    handleRenameFolderWrapper,
    prepareRecheckWrapper,
    prepareReannounceWrapper,
    handleRecheckWrapper,
    handleReannounceWrapper,
    handleTmmConfirmWrapper,
    handleSetShareLimitWrapper,
    handleSetSpeedLimitsWrapper,
  }
}
