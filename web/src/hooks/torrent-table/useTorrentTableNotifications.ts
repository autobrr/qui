/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Category, Torrent, TorrentCounts, TorrentFilters } from "@/types"
import { useEffect, useRef } from "react"

type FilteredDataUpdate = (
  torrents: Torrent[],
  total: number,
  counts?: TorrentCounts,
  categories?: Record<string, Category>,
  tags?: string[],
  useSubcategories?: boolean,
  supportsTrackerHealth?: boolean
) => void

type SelectionChange = (
  selectedHashes: string[],
  selectedTorrents: Torrent[],
  isAllSelected: boolean,
  totalSelectionCount: number,
  excludeHashes: string[],
  excludeTargets: Array<{ instanceId: number; hash: string }>,
  selectedTotalSize: number,
  selectionFilters?: TorrentFilters
) => void

export interface UseTorrentTableNotificationsParams {
  // onFilteredDataUpdate effect inputs
  onFilteredDataUpdate?: FilteredDataUpdate
  isLoading: boolean
  instanceId: number
  counts?: TorrentCounts
  categories?: Record<string, Category>
  tags?: string[]
  totalCount: number
  torrents: Torrent[]
  // The fully-derived "subcategories active right now" flag (folds in capabilities,
  // the always-enabled flag, and the user's use_subcategories preference) — the same
  // value the table/cards render from, so the parent receives a consistent flag.
  allowSubcategories: boolean
  supportsTrackerHealth: boolean
  // onSelectionChange effect inputs
  onSelectionChange?: SelectionChange
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  isAllSelected: boolean
  effectiveSelectionCount: number
  selectAllExcludeHashes?: string[]
  selectAllExcludedTargets: Array<{ instanceId: number; hash: string }>
  selectedTotalSize: number
  selectAllFilters?: TorrentFilters
  filters?: TorrentFilters
}

/**
 * Fires the parent-facing onFilteredDataUpdate / onSelectionChange callbacks
 * when the relevant state changes.
 *
 * onFilteredDataUpdate uses an internal lastMetadataRef no-op suppression: it
 * caches the last-emitted metadata per instance and skips the callback when
 * nothing meaningful changed. This is load-bearing for re-render-loop safety —
 * even if the parent supplies an unmemoized callback identity, an unchanged
 * payload is not re-emitted. Keep the comparison and the instanceId cache-key
 * reset byte-for-byte; do not add instanceId to the effect deps.
 *
 * Parents should still memoize the callbacks where practical.
 */
export function useTorrentTableNotifications({
  onFilteredDataUpdate,
  isLoading,
  instanceId,
  counts,
  categories,
  tags,
  totalCount,
  torrents,
  allowSubcategories,
  supportsTrackerHealth,
  onSelectionChange,
  selectedHashes,
  selectedTorrents,
  isAllSelected,
  effectiveSelectionCount,
  selectAllExcludeHashes,
  selectAllExcludedTargets,
  selectedTotalSize,
  selectAllFilters,
  filters,
}: UseTorrentTableNotificationsParams): void {
  const lastMetadataRef = useRef<{
    instanceId?: number
    counts?: TorrentCounts
    categories?: Record<string, Category>
    tags?: string[]
    totalCount?: number
    torrentsLength?: number
    allowSubcategories?: boolean
    supportsTrackerHealth?: boolean
  }>({})

  // Call the callback when filtered data updates
  useEffect(() => {
    if (!onFilteredDataUpdate || isLoading) {
      return
    }

    const cachedMetadata =
      lastMetadataRef.current.instanceId === instanceId? lastMetadataRef.current: ({} as typeof lastMetadataRef.current)

    const nextCounts = counts ?? cachedMetadata.counts
    const nextCategories = categories ?? cachedMetadata.categories
    const nextTags = tags ?? cachedMetadata.tags
    const previousAllowSubcategories = cachedMetadata.allowSubcategories ?? false
    const previousSupportsTrackerHealth = cachedMetadata.supportsTrackerHealth ?? false
    const nextAllowSubcategories = allowSubcategories
    const nextSupportsTrackerHealth = supportsTrackerHealth
    const nextTotalCount = totalCount

    const hasAnyMetadata =
      nextCounts !== undefined ||
      nextCategories !== undefined ||
      nextTags !== undefined ||
      nextAllowSubcategories !== undefined
    const hasExistingTorrents = torrents.length > 0

    if (!hasAnyMetadata && !hasExistingTorrents) {
      return
    }

    const metadataChanged =
      nextCounts !== cachedMetadata.counts ||
      nextCategories !== cachedMetadata.categories ||
      nextTags !== cachedMetadata.tags ||
      nextAllowSubcategories !== previousAllowSubcategories ||
      nextSupportsTrackerHealth !== previousSupportsTrackerHealth ||
      nextTotalCount !== cachedMetadata.totalCount

    const torrentsLengthChanged = torrents.length !== (cachedMetadata.torrentsLength ?? -1)

    if (!metadataChanged && !torrentsLengthChanged) {
      return
    }

    onFilteredDataUpdate(
      torrents,
      totalCount,
      nextCounts,
      nextCategories,
      nextTags,
      nextAllowSubcategories,
      nextSupportsTrackerHealth
    )

    lastMetadataRef.current = {
      instanceId,
      counts: nextCounts,
      categories: nextCategories,
      tags: nextTags,
      totalCount: nextTotalCount,
      torrentsLength: torrents.length,
      allowSubcategories: nextAllowSubcategories,
      supportsTrackerHealth: nextSupportsTrackerHealth,
    }
    // instanceId is intentionally NOT a dependency — it's only a ref cache key
    // (adding it would change the effect's re-run timing).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [counts, categories, tags, totalCount, torrents, isLoading, onFilteredDataUpdate, allowSubcategories, supportsTrackerHealth])

  // Call the callback when selection state changes
  useEffect(() => {
    if (onSelectionChange) {
      onSelectionChange(
        selectedHashes,
        selectedTorrents,
        isAllSelected,
        effectiveSelectionCount,
        selectAllExcludeHashes ?? [],
        isAllSelected ? selectAllExcludedTargets : [],
        selectedTotalSize,
        selectAllFilters ?? filters
      )
    }
  }, [onSelectionChange, selectedHashes, selectedTorrents, isAllSelected, effectiveSelectionCount, selectAllExcludeHashes, selectAllExcludedTargets, selectedTotalSize, selectAllFilters, filters])
}
