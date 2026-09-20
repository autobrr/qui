/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SelectionRow } from "@/hooks/torrent-table/useTorrentSelection"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import { combineFilterExpr } from "@/lib/torrent-filters"
import { getTotalSize } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import type { Torrent, TorrentFilters } from "@/types"
import { useMemo, useRef } from "react"

// Stable results for the no-selection steady state. The memos below depend on
// sortedTorrents, which is a fresh array on every stream tick; without these
// constants an empty selection would still produce a new [] per tick and defeat
// the referential stability that row/menu memoization relies on.
const EMPTY_HASHES: string[] = []
const EMPTY_TORRENTS: Torrent[] = []
const EMPTY_TARGETS: TorrentActionTarget[] = []

export interface UseTorrentSelectionDerivationsParams {
  isAllSelected: boolean
  excludedFromSelectAll: Set<string>
  selectedRowIds: string[]
  selectedRowIdSet: Set<string>
  getSelectionIdentity: (torrent: Torrent) => string
  getVisibleRows: () => SelectionRow[]
  sortedTorrents: Torrent[]
  columnFiltersExpr: string | null
  // True when the table, not the backend, applies the column filters (cross-seed mode).
  clientSideFiltering?: boolean
  filters: TorrentFilters
  stats?: { totalSize?: number }
  totalCount: number
  isCrossInstanceEndpoint?: boolean
  instanceId: number
  contextTorrents: Torrent[]
}

/**
 * Derives everything bulk actions and dialogs need from the current selection:
 * the resolved hashes/torrents, counts, sizes, and the select-all targeting
 * filters/excludes.
 *
 * `selectAllFilters` always combines column filters with `filters.expr`
 * (#1925), unlike the list expression in `useTorrentTableFilterExpr`.
 */
export function useTorrentSelectionDerivations({
  isAllSelected,
  excludedFromSelectAll,
  selectedRowIds,
  selectedRowIdSet,
  getSelectionIdentity,
  getVisibleRows,
  sortedTorrents,
  columnFiltersExpr,
  clientSideFiltering = false,
  filters,
  stats,
  totalCount,
  isCrossInstanceEndpoint,
  instanceId,
  contextTorrents,
}: UseTorrentSelectionDerivationsParams) {
  // Read the latest row accessor without making it a memo dependency.
  const getVisibleRowsRef = useRef(getVisibleRows)
  getVisibleRowsRef.current = getVisibleRows

  // Get selected torrent hashes - handle both regular selection and "select all" mode
  const selectedHashes = useMemo((): string[] => {
    if (isAllSelected) {
      // The table's row model, not sortedTorrents: in cross-seed mode the column
      // filter applies client-side, and select-all is the visible set (#1925).
      return getVisibleRowsRef.current()
        .map(row => row.original)
        .filter(torrent => !excludedFromSelectAll.has(getSelectionIdentity(torrent)))
        .map(torrent => torrent.hash)
    } else {
      if (selectedRowIdSet.size === 0) {
        return EMPTY_HASHES
      }
      // Regular selection mode - get hashes from selected torrents directly
      const tableRows = getVisibleRowsRef.current()
      return tableRows
        .filter(row => selectedRowIdSet.has(row.id))
        .map(row => row.original.hash)
    }
    // The row model is read through the ref; sortedTorrents and columnFiltersExpr are its inputs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRowIdSet, isAllSelected, excludedFromSelectAll, sortedTorrents, columnFiltersExpr, getSelectionIdentity])

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // The backend total does not know a client-side column filter; the
      // visible rows do, and a select-all action reaches exactly those.
      // ponytail: counts loaded rows only, so a cross-seed set past one page
      // (300) under-reports; a cross-seed expression is one torrent's siblings.
      if (clientSideFiltering) {
        return selectedHashes.length
      }
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the computed selectedHashes length
      return selectedRowIds.length
    }
  }, [isAllSelected, clientSideFiltering, selectedHashes.length, totalCount, excludedFromSelectAll.size, selectedRowIds.length])

  // Get selected torrents
  const selectedTorrents = useMemo((): Torrent[] => {
    if (isAllSelected) {
      return getVisibleRowsRef.current()
        .map(row => row.original)
        .filter(t => !excludedFromSelectAll.has(getSelectionIdentity(t)))
    } else {
      if (selectedRowIdSet.size === 0) {
        return EMPTY_TORRENTS
      }
      // Regular selection mode
      return getVisibleRowsRef.current()
        .filter(row => selectedRowIdSet.has(row.id))
        .map(row => row.original)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRowIdSet, sortedTorrents, columnFiltersExpr, isAllSelected, excludedFromSelectAll, getSelectionIdentity])

  // Calculate total size of selected torrents
  const selectedTotalSize = useMemo(() => {
    if (isAllSelected && !clientSideFiltering) {
      const aggregateTotalSize = stats?.totalSize ?? 0

      if (aggregateTotalSize <= 0) {
        return 0
      }

      if (excludedFromSelectAll.size === 0) {
        return aggregateTotalSize
      }

      const excludedSize = sortedTorrents.reduce((total, torrent) => {
        if (excludedFromSelectAll.has(getSelectionIdentity(torrent))) {
          return total + (torrent.size || 0)
        }
        return total
      }, 0)

      return Math.max(aggregateTotalSize - excludedSize, 0)
    }

    return getTotalSize(selectedTorrents)
  }, [isAllSelected, clientSideFiltering, stats?.totalSize, excludedFromSelectAll, sortedTorrents, selectedTorrents, getSelectionIdentity])
  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  // Size shown in destructive dialogs - prefer the aggregate when select-all is active
  const deleteDialogTotalSize = useMemo(() => {
    if (isAllSelected) {
      if (selectedTotalSize > 0) {
        return selectedTotalSize
      }

      if (contextTorrents.length > 0) {
        return getTotalSize(contextTorrents)
      }

      return 0
    }

    if (contextTorrents.length > 0) {
      return getTotalSize(contextTorrents)
    }

    return selectedTotalSize
  }, [isAllSelected, selectedTotalSize, contextTorrents])
  const deleteDialogFormattedSize = useMemo(() => formatBytes(deleteDialogTotalSize), [deleteDialogTotalSize])

  const selectAllFilters = useMemo(() => {
    if (!isAllSelected) {
      return undefined
    }

    return {
      ...filters,
      expr: combineFilterExpr(columnFiltersExpr, filters.expr) ?? "",
    }
  }, [isAllSelected, filters, columnFiltersExpr])

  const selectAllExcludedTargets = useMemo(() => {
    if (!isAllSelected || excludedFromSelectAll.size === 0) {
      return EMPTY_TARGETS
    }
    const excludedTorrents = sortedTorrents.filter(torrent => excludedFromSelectAll.has(getSelectionIdentity(torrent)))
    return buildTorrentActionTargets(excludedTorrents, instanceId)
  }, [isAllSelected, excludedFromSelectAll, sortedTorrents, instanceId, getSelectionIdentity])

  const selectAllExcludeHashes = useMemo(() => {
    if (!isAllSelected || excludedFromSelectAll.size === 0 || isCrossInstanceEndpoint) {
      return undefined
    }

    return Array.from(excludedFromSelectAll)
  }, [isAllSelected, excludedFromSelectAll, isCrossInstanceEndpoint])

  return {
    selectedHashes,
    effectiveSelectionCount,
    selectedTorrents,
    selectedTotalSize,
    selectedFormattedSize,
    deleteDialogTotalSize,
    deleteDialogFormattedSize,
    selectAllFilters,
    selectAllExcludedTargets,
    selectAllExcludeHashes,
  }
}
