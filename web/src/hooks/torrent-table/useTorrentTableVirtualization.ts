/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useKeyboardNavigation } from "@/hooks/useKeyboardNavigation"
import type { ViewMode } from "@/hooks/usePersistedCompactViewState"
import { viewModeRowHeight } from "@/lib/torrent-table/row-height"
import type { Torrent } from "@/types"
import type { Row } from "@tanstack/react-table"
import { useVirtualizer, type VirtualItem, type Virtualizer } from "@tanstack/react-virtual"
import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from "react"

export interface UseTorrentTableVirtualizationParams {
  /** The table's current row model rows (by value — this hook runs after the table). */
  rows: Row<Torrent>[]
  desktopViewMode: ViewMode
  /** Full filtered dataset length (drives overscan + the progressive target), NOT rows.length. */
  sortedTorrentsLength: number
  hasLoadedAll: boolean
  isLoadingMore: boolean
  backendLoadMore?: () => void
}

export interface TorrentTableVirtualization {
  parentRef: RefObject<HTMLDivElement | null>
  virtualizer: Virtualizer<HTMLDivElement, Element>
  virtualRows: VirtualItem[]
  safeLoadedRows: number
  loadMore: () => void
  loadedRows: number
  setLoadedRows: Dispatch<SetStateAction<number>>
  /** Exposed for the parent's filter-change reset effect, which clears the in-flight flag. */
  setIsLoadingMoreRows: Dispatch<SetStateAction<boolean>>
  estimatedRowHeight: number
}

/**
 * Owns the table's row virtualization and progressive loading: the virtualizer
 * instance, the scroll container ref, the local `loadedRows` window (+ backend
 * pagination handoff), row-height estimation, and the measure/keyboard-nav
 * wiring. `loadedRows`/`setLoadedRows` are returned so the filter-lifecycle hook
 * can share the same window.
 *
 * The `virtualizer` is returned by reference (the literal useVirtualizer result):
 * it is a dependency of several effects and of keyboard navigation, so a fresh
 * wrapper per render would loop those effects. Keep it un-wrapped.
 */
export function useTorrentTableVirtualization({
  rows,
  desktopViewMode,
  sortedTorrentsLength,
  hasLoadedAll,
  isLoadingMore,
  backendLoadMore,
}: UseTorrentTableVirtualizationParams): TorrentTableVirtualization {
  const [loadedRows, setLoadedRows] = useState(100)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)
  const parentRef = useRef<HTMLDivElement>(null)

  // Load more rows as user scrolls (progressive loading + backend pagination)
  const loadMore = useCallback((): void => {
    // First, try to load more from virtual scrolling if we have more local data
    if (loadedRows < sortedTorrentsLength) {
      // Prevent concurrent loads
      if (isLoadingMoreRows) {
        return
      }

      setIsLoadingMoreRows(true)

      setLoadedRows(prev => {
        const newLoadedRows = Math.min(prev + 100, sortedTorrentsLength)
        return newLoadedRows
      })

      // Reset loading flag after a short delay
      setTimeout(() => setIsLoadingMoreRows(false), 100)
    } else if (!hasLoadedAll && !isLoadingMore && backendLoadMore) {
      // If we've displayed all local data but there's more on backend, load next page
      backendLoadMore()
    }
  }, [sortedTorrentsLength, isLoadingMoreRows, loadedRows, hasLoadedAll, isLoadingMore, backendLoadMore])

  // Ensure loadedRows never exceeds actual data length
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

      // Only trigger loadMore when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive loadMore calls during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling

      if (shouldCheckLoadMore && lastItem && lastItem.index >= safeLoadedRows - 50) {
        // Load more if we're near the end of virtual rows OR if we might need more data from backend
        if (safeLoadedRows < rows.length || (!hasLoadedAll && !isLoadingMore)) {
          loadMore();
        }
      }
    },
  })

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, virtualizer])

  // Recalculate virtualized row sizes when view mode changes
  useEffect(() => {
    virtualizer.measure()
  }, [desktopViewMode, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  // Reset loaded rows when data changes significantly
  useEffect(() => {
    // Always ensure loadedRows is at least 100 (or total length if less)
    const targetRows = Math.min(100, sortedTorrentsLength)

    setLoadedRows(prev => {
      if (sortedTorrentsLength === 0) {
        // No data, reset to 0
        return 0
      } else if (prev === 0) {
        // Initial load
        return targetRows
      } else if (prev < targetRows) {
        // Not enough rows loaded, load at least 100
        return targetRows
      }
      // Don't reset loadedRows backward due to temporary server data fluctuations
      // Progressive loading should be independent of server data variations
      return prev
    })

    // Force virtualizer to recalculate
    virtualizer.measure()
  }, [sortedTorrentsLength, virtualizer])

  // Set up keyboard navigation (PageUp/Down, Home/End)
  useKeyboardNavigation({
    parentRef,
    virtualizer,
    safeLoadedRows,
    hasLoadedAll,
    isLoadingMore,
    loadMore,
    estimatedRowHeight,
  })

  return {
    parentRef,
    virtualizer,
    virtualRows,
    safeLoadedRows,
    loadMore,
    loadedRows,
    setLoadedRows,
    setIsLoadingMoreRows,
    estimatedRowHeight,
  }
}
