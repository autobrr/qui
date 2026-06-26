/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SelectionRow } from "@/hooks/torrent-table/useTorrentSelection"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import { getTotalSize } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import type { Torrent, TorrentFilters } from "@/types"
import { useMemo, useRef } from "react"

export interface UseTorrentSelectionDerivationsParams {
  isAllSelected: boolean
  excludedFromSelectAll: Set<string>
  selectedRowIds: string[]
  selectedRowIdSet: Set<string>
  getSelectionIdentity: (torrent: Torrent) => string
  getVisibleRows: () => SelectionRow[]
  sortedTorrents: Torrent[]
  columnFiltersExpr: string | null
  filters?: TorrentFilters
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
 * #1925 PIN: `selectAllFilters` must ALWAYS combine `columnFiltersExpr` with
 * `filters.expr` when both are present. This is deliberately DIFFERENT from
 * `useTorrentTableFilterExpr`'s `combinedFiltersExpr`, which early-returns the
 * cross-seed expression alone. Collapsing the two re-introduces #1925 (bulk
 * actions matching more torrents than the user sees).
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
      // When all are selected, return all currently loaded hashes minus exclusions
      // This is needed for actions to work properly
      return sortedTorrents
        .filter(torrent => !excludedFromSelectAll.has(getSelectionIdentity(torrent)))
        .map(torrent => torrent.hash)
    } else {
      // Regular selection mode - get hashes from selected torrents directly
      const tableRows = getVisibleRowsRef.current()
      return tableRows
        .filter(row => selectedRowIdSet.has(row.id))
        .map(row => row.original.hash)
    }
  }, [selectedRowIdSet, isAllSelected, excludedFromSelectAll, sortedTorrents, getSelectionIdentity])

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // When all selected, count is total minus exclusions
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the computed selectedHashes length
      return selectedRowIds.length
    }
  }, [isAllSelected, totalCount, excludedFromSelectAll.size, selectedRowIds.length])

  // Get selected torrents
  const selectedTorrents = useMemo((): Torrent[] => {
    if (isAllSelected) {
      // When all are selected, return all torrents minus exclusions
      return sortedTorrents.filter(t => !excludedFromSelectAll.has(getSelectionIdentity(t)))
    } else {
      // Regular selection mode
      return getVisibleRowsRef.current()
        .filter(row => selectedRowIdSet.has(row.id))
        .map(row => row.original)
    }
  }, [selectedRowIdSet, sortedTorrents, isAllSelected, excludedFromSelectAll, getSelectionIdentity])

  // Calculate total size of selected torrents
  const selectedTotalSize = useMemo(() => {
    if (isAllSelected) {
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
  }, [isAllSelected, stats?.totalSize, excludedFromSelectAll, sortedTorrents, selectedTorrents, getSelectionIdentity])
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

    // Combine both column filters and filter expressions (e.g. cross-seed hash filters)
    // so select-all operations target exactly the visible set.
    // Using ?? here would drop filters.expr when columnFiltersExpr is present,
    // causing bulk actions to match more torrents than the user sees.
    const combinedExpr = (columnFiltersExpr && filters?.expr)? `(${columnFiltersExpr}) && (${filters.expr})`: (columnFiltersExpr || filters?.expr)

    if (filters) {
      return {
        ...filters,
        expr: combinedExpr ?? filters.expr ?? "",
      }
    }

    if (combinedExpr == null) {
      return undefined
    }

    return {
      status: [],
      excludeStatus: [],
      categories: [],
      excludeCategories: [],
      tags: [],
      excludeTags: [],
      trackers: [],
      excludeTrackers: [],
      expr: combinedExpr,
    }
  }, [isAllSelected, filters, columnFiltersExpr])

  const selectAllExcludedTargets = useMemo(() => {
    if (!isAllSelected || excludedFromSelectAll.size === 0) {
      return []
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
