/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useKeyboardNavigation } from "@/hooks/useKeyboardNavigation"
import type { ViewMode } from "@/hooks/usePersistedCompactViewState"
import { viewModeRowHeight } from "@/lib/torrent-table/row-height"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import { useVirtualizer, type VirtualItem, type Virtualizer } from "@tanstack/react-virtual"
import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react"

export interface UseTorrentTableVirtualizationParams {
  rows: TorrentRow[]
  desktopViewMode: ViewMode
  sortedTorrentsLength: number
  loadedRows: number
  hasMoreRows: boolean
  reportNearEnd: () => void
  hasLoadedAll: boolean
  isLoadingMore: boolean
}

export interface TorrentTableVirtualization {
  parentRef: RefObject<HTMLDivElement | null>
  virtualizer: Virtualizer<HTMLDivElement, Element>
  virtualRows: VirtualItem[]
  safeLoadedRows: number
  estimatedRowHeight: number
}

export function useTorrentTableVirtualization({
  rows,
  desktopViewMode,
  sortedTorrentsLength,
  loadedRows,
  hasMoreRows,
  reportNearEnd,
  hasLoadedAll,
  isLoadingMore,
}: UseTorrentTableVirtualizationParams): TorrentTableVirtualization {
  const parentRef = useRef<HTMLDivElement>(null)

  const safeLoadedRows = Math.min(loadedRows, rows.length)

  const estimatedRowHeight = useMemo(() => viewModeRowHeight(desktopViewMode), [desktopViewMode])

  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimatedRowHeight,
    overscan: sortedTorrentsLength > 50000 ? 3 : sortedTorrentsLength > 10000 ? 5 : sortedTorrentsLength > 1000 ? 10 : 15,
    getItemKey: useCallback((index: number) => {
      const row = rows[index]
      if (!row) return `loading-${index}`
      return row.id
    }, [rows]),
    onChange: (instance) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      const scrolledPastRows = (instance.scrollOffset ?? 0) > instance.getTotalSize()

      if (!scrolledPastRows && lastItem && lastItem.index >= safeLoadedRows - 50 && hasMoreRows) {
        reportNearEnd();
      }
    },
  })

  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, sortedTorrentsLength, virtualizer])

  useEffect(() => {
    virtualizer.measure()
  }, [desktopViewMode, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  useKeyboardNavigation({
    parentRef,
    virtualizer,
    safeLoadedRows,
    hasLoadedAll,
    isLoadingMore,
    loadMore: reportNearEnd,
    estimatedRowHeight,
  })

  return {
    parentRef,
    virtualizer,
    virtualRows,
    safeLoadedRows,
    estimatedRowHeight,
  }
}
