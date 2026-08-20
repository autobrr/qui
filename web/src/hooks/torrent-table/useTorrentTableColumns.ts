/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createColumns, type TableViewMode } from "@/components/torrents/TorrentTableColumns"
import type { SpeedUnit } from "@/lib/speedUnits"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import type { AppPreferences, Torrent } from "@/types"
import type { ColumnDef } from "@tanstack/react-table"
import type { TFunction } from "i18next"
import { type RefObject, useMemo } from "react"

export interface UseTorrentTableColumnsParams {
  // Selection enhancers (from useTorrentSelection)
  shiftPressedRef: RefObject<boolean>
  lastSelectedIndexRef: RefObject<number | null>
  handleSelectAll: () => void
  isSelectAllChecked: boolean
  isSelectAllIndeterminate: boolean
  handleRowSelection: (selectionIdentity: string, checked: boolean, rowId?: string) => void
  getSelectionIdentity: (torrent: Torrent) => string
  isAllSelected: boolean
  excludedFromSelectAll: Set<string>
  // Display / format
  incognitoMode: boolean
  speedUnit: SpeedUnit
  trackerIcons?: Record<string, string>
  formatTimestamp: (timestamp: number, relative?: boolean) => string
  preferences?: AppPreferences | null
  supportsTrackerHealth: boolean
  isUnifiedView: boolean
  isCrossInstanceEndpoint?: boolean
  desktopViewMode: string
  trackerCustomizationLookup?: TrackerCustomizationLookup
  isReadOnly: boolean
  t: TFunction
  // Data (for the duplicate-identity counts that feed the table's getRowId)
  sortedTorrents: Torrent[]
}

export interface TorrentTableColumns {
  columns: ColumnDef<Torrent>[]
  torrentIdentityCounts: Map<string, number>
}

/**
 * Builds the table column definitions (via createColumns) and the per-identity
 * duplicate counts the table's getRowId uses. Kept as a hook so the heavy
 * createColumns call stays memoized over its exact inputs and the torrent table
 * orchestrator only consumes the result.
 */
export function useTorrentTableColumns({
  shiftPressedRef,
  lastSelectedIndexRef,
  handleSelectAll,
  isSelectAllChecked,
  isSelectAllIndeterminate,
  handleRowSelection,
  getSelectionIdentity,
  isAllSelected,
  excludedFromSelectAll,
  incognitoMode,
  speedUnit,
  trackerIcons,
  formatTimestamp,
  preferences,
  supportsTrackerHealth,
  isUnifiedView,
  isCrossInstanceEndpoint,
  desktopViewMode,
  trackerCustomizationLookup,
  isReadOnly,
  t,
  sortedTorrents,
}: UseTorrentTableColumnsParams): TorrentTableColumns {
  // Memoize columns to avoid unnecessary recalculations
  const columns = useMemo(
    () => createColumns(incognitoMode, {
      shiftPressedRef,
      lastSelectedIndexRef,
      // Pass custom selection handlers
      customSelectAll: {
        onSelectAll: handleSelectAll,
        isAllSelected: isSelectAllChecked,
        isIndeterminate: isSelectAllIndeterminate,
      },
      onRowSelection: handleRowSelection,
      getSelectionIdentity,
      isAllSelected,
      excludedFromSelectAll,
    }, speedUnit, trackerIcons, (timestamp: number) => formatTimestamp(timestamp, true), preferences, supportsTrackerHealth, isUnifiedView && isCrossInstanceEndpoint, desktopViewMode as TableViewMode, trackerCustomizationLookup, !isReadOnly, t),
    // shiftPressedRef/lastSelectedIndexRef are stable refs (passed in); listed to satisfy exhaustive-deps.
    [shiftPressedRef, lastSelectedIndexRef, incognitoMode, speedUnit, trackerIcons, formatTimestamp, handleSelectAll, isSelectAllChecked, isSelectAllIndeterminate, handleRowSelection, getSelectionIdentity, isAllSelected, excludedFromSelectAll, preferences, supportsTrackerHealth, isUnifiedView, isCrossInstanceEndpoint, desktopViewMode, trackerCustomizationLookup, isReadOnly, t]
  )

  const torrentIdentityCounts = useMemo(() => {
    const counts = new Map<string, number>()

    for (const torrent of sortedTorrents) {
      const baseIdentity = torrent.hash ?? torrent.infohash_v1 ?? torrent.infohash_v2
      if (!baseIdentity) continue
      counts.set(baseIdentity, (counts.get(baseIdentity) ?? 0) + 1)
    }

    return counts
  }, [sortedTorrents])

  return { columns, torrentIdentityCounts }
}
