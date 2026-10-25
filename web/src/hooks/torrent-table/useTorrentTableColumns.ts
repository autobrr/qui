/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createColumns, type TableViewMode } from "@/components/torrents/TorrentTableColumns"
import type { TorrentTableColumnDef } from "@/components/torrents/tanstackTableFeatures"
import type { SpeedUnit } from "@/lib/speedUnits"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import type { Torrent } from "@/types"
import type { TFunction } from "i18next"
import { useMemo } from "react"

export interface UseTorrentTableColumnsParams {
  // Selection (from useTorrentSelection)
  handleSelectAll: () => void
  isSelectAllChecked: boolean
  isSelectAllIndeterminate: boolean
  isTorrentSelected: (torrent: Torrent) => boolean
  handleCheckboxPointerDown: (event: React.PointerEvent<HTMLElement>) => void
  handleCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
  // Display / format
  incognitoMode: boolean
  speedUnit: SpeedUnit
  trackerIcons?: Record<string, string>
  formatTimestamp: (timestamp: number, relative?: boolean) => string
  instanceMaxRatio?: number
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
  columns: TorrentTableColumnDef[]
  torrentIdentityCounts: Map<string, number>
}

/**
 * Builds the table column definitions (via createColumns) and the per-identity
 * duplicate counts the table's getRowId uses. Kept as a hook so the heavy
 * createColumns call stays memoized over its exact inputs and the torrent table
 * orchestrator only consumes the result.
 */
export function useTorrentTableColumns({
  handleSelectAll,
  isSelectAllChecked,
  isSelectAllIndeterminate,
  isTorrentSelected,
  handleCheckboxPointerDown,
  handleCheckboxChange,
  incognitoMode,
  speedUnit,
  trackerIcons,
  formatTimestamp,
  instanceMaxRatio,
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
    () => createColumns({
      incognitoMode,
      selection: isReadOnly ? undefined : {
        onSelectAll: handleSelectAll,
        isAllChecked: isSelectAllChecked,
        isIndeterminate: isSelectAllIndeterminate,
        isSelected: isTorrentSelected,
        onCheckboxPointerDown: handleCheckboxPointerDown,
        onCheckboxChange: handleCheckboxChange,
      },
      speedUnit,
      trackerIcons,
      formatTimestamp: (timestamp: number) => formatTimestamp(timestamp, true),
      instanceMaxRatio,
      supportsTrackerHealth,
      showInstanceColumn: isUnifiedView && isCrossInstanceEndpoint,
      viewMode: desktopViewMode as TableViewMode,
      trackerCustomizationLookup,
    }, t),
    [incognitoMode, speedUnit, trackerIcons, formatTimestamp, handleSelectAll, isSelectAllChecked, isSelectAllIndeterminate, isTorrentSelected, handleCheckboxPointerDown, handleCheckboxChange, instanceMaxRatio, supportsTrackerHealth, isUnifiedView, isCrossInstanceEndpoint, desktopViewMode, trackerCustomizationLookup, isReadOnly, t]
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
