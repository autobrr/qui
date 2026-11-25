/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ListFacts, TorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import type { ActionTarget } from "@/lib/torrent-selection"
import type { Category, Torrent, TorrentCounts } from "@/types"
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

export interface UseTorrentTableNotificationsParams {
  onFilteredDataUpdate?: FilteredDataUpdate
  isLoading: boolean
  instanceId: number
  counts?: TorrentCounts
  categories?: Record<string, Category>
  tags?: string[]
  totalCount: number
  torrents: Torrent[]
  allowSubcategories: boolean
  supportsTrackerHealth: boolean
  onSelectionChange?: (summary: TorrentSelectionSummary | null) => void
  actionTarget: ActionTarget
  totalSize: number
  excludedSize: number
  instanceIds?: number[]
  listFacts: ListFacts
}

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
  actionTarget,
  totalSize,
  excludedSize,
  instanceIds,
  listFacts,
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [counts, categories, tags, totalCount, torrents, isLoading, onFilteredDataUpdate, allowSubcategories, supportsTrackerHealth])

  useEffect(() => {
    onSelectionChange?.({ actionTarget, totalSize, excludedSize, instanceId, instanceIds, listFacts })
  }, [onSelectionChange, actionTarget, totalSize, excludedSize, instanceId, instanceIds, listFacts])

  useEffect(() => () => onSelectionChange?.(null), [onSelectionChange])
}
