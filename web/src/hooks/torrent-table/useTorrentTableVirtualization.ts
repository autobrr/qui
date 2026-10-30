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
  /** The table's current row model rows (by value — this hook runs after the table). */
  rows: TorrentRow[]
  desktopViewMode: ViewMode
  /** Full filtered dataset length (drives overscan and remeasure), NOT rows.length. */
  sortedTorrentsLength: number
  /** The load-more window of useTorrentListController. */
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

/**
 * Owns the table's row virtualization: the virtualizer instance, the scroll
 * container ref, row-height estimation, and the measure/keyboard-nav wiring.
 * useTorrentListController owns the loaded-rows window; this hook reports when
 * the user scrolls near its end.
 *
 * The `virtualizer` is returned by reference (the literal useVirtualizer result):
 * it is a dependency of several effects and of keyboard navigation, so a fresh
 * wrapper per render would loop those effects. Keep it un-wrapped.
 */
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

  // In cross-seed mode the column filters run client-side, so the table can
  // show fewer rows than the list holds.
  const safeLoadedRows = Math.min(loadedRows, rows.length)

  // Compute estimated row height based on view mode - used by virtualizer and keyboard navigation
  const estimatedRowHeight = useMemo(() => viewModeRowHeight(desktopViewMode), [desktopViewMode])

  // useVirtualizer must be called at the top level, not inside useMemo
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimatedRowHeight,
    // Optimized overscan based on TanStack Virtual recommendations
    // Start small and adjust based on dataset size and performance
    overscan: sortedTorrentsLength > 50000 ? 3 : sortedTorrentsLength > 10000 ? 5 : sortedTorrentsLength > 1000 ? 10 : 15,
    // Provide a key to help with item tracking - use hash with index for uniqueness
    getItemKey: useCallback((index: number) => {
      const row = rows[index]
      if (!row) return `loading-${index}`
      return row.id
    }, [rows]),
    // Optimized onChange handler following TanStack Virtual best practices
    onChange: (instance, sync) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      // Only report the end when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive reports during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling
      // A scroll position past the last row belongs to the previous list: the
      // window shrank before the Layout scrolled back to the top.
      const scrolledPastRows = (instance.scrollOffset ?? 0) > instance.getTotalSize()

      if (shouldCheckLoadMore && !scrolledPastRows && lastItem && lastItem.index >= safeLoadedRows - 50 && hasMoreRows) {
        reportNearEnd();
      }
    },
  })

  // Force virtualizer to recalculate when the count or the data length changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, sortedTorrentsLength, virtualizer])

  // Recalculate virtualized row sizes when view mode changes
  useEffect(() => {
    virtualizer.measure()
  }, [desktopViewMode, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  // Set up keyboard navigation (PageUp/Down, Home/End)
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
