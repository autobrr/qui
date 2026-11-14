/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SelectionRow } from "@/hooks/torrent-table/useTorrentSelection"
import { buildActionTarget, EMPTY_ACTION_TARGET, rememberTickedTorrents, type ActionTargetScope, type TorrentSelectionState } from "@/lib/torrent-selection"
import { getTotalSize } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import type { Torrent, TorrentFilters } from "@/types"
import { useMemo, useRef } from "react"

export interface UseTorrentSelectionDerivationsParams {
  selection: TorrentSelectionState
  getSelectionIdentity: (torrent: Torrent) => string
  getVisibleRows: () => SelectionRow[]
  /** Every loaded row, the hidden copies of a Copy group included. */
  sortedTorrents: Torrent[]
  /** The list holds Copy groups: the Action target then reads every loaded row, not the row model. */
  isGrouped?: boolean
  columnFiltersExpr: string | null
  // True when the table, not the backend, applies the column filters (cross-seed mode).
  clientSideFiltering?: boolean
  filters: TorrentFilters
  search: string
  stats?: { totalSize?: number }
  totalCount: number
  isCrossInstanceEndpoint?: boolean
  instanceId: number
  instanceIds?: number[]
}

/**
 * Derives the Action target of the desktop table (ADR 0034) and the sizes that
 * the status bar shows, and the size facts that the delete dialog needs.
 */
export function useTorrentSelectionDerivations({
  selection,
  getSelectionIdentity,
  getVisibleRows,
  sortedTorrents,
  isGrouped = false,
  columnFiltersExpr,
  clientSideFiltering = false,
  filters,
  search,
  stats,
  totalCount,
  isCrossInstanceEndpoint = false,
  instanceId,
  instanceIds,
}: UseTorrentSelectionDerivationsParams) {
  // Read the latest row accessor without making it a memo dependency.
  const getVisibleRowsRef = useRef(getVisibleRows)
  getVisibleRowsRef.current = getVisibleRows

  const tickedTorrentsRef = useRef(new Map<string, Torrent>())

  const actionTarget = useMemo(() => {
    const { isAllSelected, selected } = selection
    if (!isAllSelected && selected.size === 0) {
      // A stable result for the steady state: sortedTorrents is a fresh array on
      // every stream tick, and row and menu memoization compares this reference.
      tickedTorrentsRef.current.clear()
      return EMPTY_ACTION_TARGET
    }
    // On a flat list the table's row model, not sortedTorrents: in cross-seed mode
    // the column filter applies client-side, and select-all is the visible set (#1925).
    // A grouped list never filters in the browser, and its row model leaves out the hidden copies.
    const loaded = isGrouped ? sortedTorrents : getVisibleRowsRef.current().map(row => row.original)
    const ticked = isAllSelected ? [] : rememberTickedTorrents(tickedTorrentsRef.current, selected, loaded, getSelectionIdentity)
    const scope: ActionTargetScope = {
      instanceId,
      instanceIds,
      filters,
      columnFilters: { expr: columnFiltersExpr, inBrowser: clientSideFiltering },
      search,
      isCrossInstance: isCrossInstanceEndpoint,
    }
    return buildActionTarget(selection, scope, { loaded, ticked, totalCount })
  }, [selection, sortedTorrents, isGrouped, columnFiltersExpr, clientSideFiltering, filters, search, totalCount, isCrossInstanceEndpoint, instanceId, instanceIds, getSelectionIdentity])

  const { isAllSelected, excluded } = selection
  // The table filters in the browser in cross-seed mode, so the total of the filter does not apply.
  const totalSize = isAllSelected && !clientSideFiltering ? stats?.totalSize ?? 0 : 0
  const excludedSize = useMemo(() => {
    if (!isAllSelected || clientSideFiltering || excluded.size === 0) {
      return 0
    }
    return sortedTorrents.reduce((total, torrent) => excluded.has(getSelectionIdentity(torrent)) ? total + (torrent.size || 0) : total, 0)
  }, [isAllSelected, clientSideFiltering, excluded, sortedTorrents, getSelectionIdentity])

  const selectedTotalSize = useMemo(
    () => isAllSelected && !clientSideFiltering ? Math.max(totalSize - excludedSize, 0) : getTotalSize(actionTarget.previewTorrents),
    [isAllSelected, clientSideFiltering, totalSize, excludedSize, actionTarget.previewTorrents]
  )
  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  return {
    actionTarget,
    selectedTotalSize,
    selectedFormattedSize,
    totalSize,
    excludedSize,
  }
}
