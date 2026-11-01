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
  sortedTorrents: Torrent[]
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
 * the status bar and the delete dialog show.
 */
export function useTorrentSelectionDerivations({
  selection,
  getSelectionIdentity,
  getVisibleRows,
  sortedTorrents,
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
    // The table's row model, not sortedTorrents: in cross-seed mode the column
    // filter applies client-side, and select-all is the visible set (#1925).
    const loaded = getVisibleRowsRef.current().map(row => row.original)
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
    // The row model is read through the ref; sortedTorrents and columnFiltersExpr are its inputs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, sortedTorrents, columnFiltersExpr, clientSideFiltering, filters, search, totalCount, isCrossInstanceEndpoint, instanceId, instanceIds, getSelectionIdentity])

  const { isAllSelected, excluded } = selection
  const selectedTotalSize = useMemo(() => {
    if (isAllSelected && !clientSideFiltering) {
      const aggregateTotalSize = stats?.totalSize ?? 0

      if (aggregateTotalSize <= 0) {
        return 0
      }

      if (excluded.size === 0) {
        return aggregateTotalSize
      }

      const excludedSize = sortedTorrents.reduce((total, torrent) => {
        if (excluded.has(getSelectionIdentity(torrent))) {
          return total + (torrent.size || 0)
        }
        return total
      }, 0)

      return Math.max(aggregateTotalSize - excludedSize, 0)
    }

    return getTotalSize(actionTarget.previewTorrents)
  }, [isAllSelected, clientSideFiltering, stats?.totalSize, excluded, sortedTorrents, actionTarget.previewTorrents, getSelectionIdentity])
  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  // Without an aggregate size, the delete dialog shows the size of the loaded torrents.
  const deleteDialogTotalSize = selectedTotalSize > 0 ? selectedTotalSize : getTotalSize(actionTarget.previewTorrents)
  const deleteDialogFormattedSize = useMemo(() => formatBytes(deleteDialogTotalSize), [deleteDialogTotalSize])

  return {
    actionTarget,
    selectedTotalSize,
    selectedFormattedSize,
    deleteDialogTotalSize,
    deleteDialogFormattedSize,
  }
}
