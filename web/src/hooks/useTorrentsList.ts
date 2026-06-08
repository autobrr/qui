/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { mergeStreamedCrossInstanceFirstPage, normalizeStreamedSnapshot } from "@/lib/cross-instance-torrents"
import { isAllInstancesScope } from "@/lib/instances"
import { mergeStreamedFirstPage } from "@/lib/stream-merge"
import type {
  AppPreferences,
  QBittorrentAppInfo,
  Torrent,
  TorrentFilters,
  TorrentResponse,
  TorrentStreamPayload
} from "@/types"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"

export const TORRENT_STREAM_POLL_INTERVAL_MS = 3000
export const TORRENT_STREAM_POLL_INTERVAL_SECONDS = Math.max(
  1,
  Math.round(TORRENT_STREAM_POLL_INTERVAL_MS / 1000)
)

// While the tab is hidden the table is invisible, so streaming a full page-0
// snapshot (up to 300 torrents) every couple of seconds is pure waste: the work is
// deferred and then burst-processed by a throttled background tab, and each frame is
// retained by anyone running DevTools with "Persist Logs" on. Pause the heavy list
// subscription once the tab has been hidden this long; the title-bar speed stream
// (limit:1) stays live so background transfer rates keep updating. The grace delay
// avoids tearing the stream down on quick tab switches; on refocus it resumes at once
// and refetchOnWindowFocus pulls fresh data immediately.
export const STREAM_HIDDEN_PAUSE_DELAY_MS = 30000

interface UseTorrentsListOptions {
  enabled?: boolean
  pollingEnabled?: boolean
  refetchIntervalInBackground?: boolean
  search?: string
  filters?: TorrentFilters
  sort?: string
  order?: "asc" | "desc"
  instanceIds?: number[]
}

// Hook that manages paginated torrent loading with stale-while-revalidate pattern
// Backend handles all caching complexity and returns fresh or stale data immediately
export function useTorrentsList(
  instanceId: number,
  options: UseTorrentsListOptions = {}
) {
  const {
    enabled = true,
    pollingEnabled = true,
    refetchIntervalInBackground = false,
    search,
    filters,
    sort = "added_on",
    order = "desc",
    instanceIds,
  } = options
  const isAllInstancesView = isAllInstancesScope(instanceId)

  const [currentPage, setCurrentPage] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [lastRequestTime, setLastRequestTime] = useState(0)
  const [lastKnownTotal, setLastKnownTotal] = useState(0)
  const [lastProcessedPage, setLastProcessedPage] = useState(-1)
  const [lastStreamSnapshot, setLastStreamSnapshot] = useState<TorrentResponse | null>(null)
  const pageSize = 300 // Load 300 at a time (backend default)
  const queryClient = useQueryClient()

  // Pause the heavy list stream while the tab is backgrounded (see
  // STREAM_HIDDEN_PAUSE_DELAY_MS). isHiddenDelayed only trips after a grace period,
  // so quick tab switches don't churn the subscription.
  const { isHiddenDelayed } = useDelayedVisibility(STREAM_HIDDEN_PAUSE_DELAY_MS)

  const metadataQueryKey = useMemo(
    () => ["instance-metadata", instanceId] as const,
    [instanceId]
  )

  const appInfoQueryKey = useMemo(
    () => ["qbittorrent-app-info", instanceId] as const,
    [instanceId]
  )

  const updateMetadataCache = useCallback(
    (source?: TorrentResponse | null) => {
      if (!source) {
        return
      }

      const hasPreferences = Object.prototype.hasOwnProperty.call(source, "preferences")
      const isCrossInstanceSource = source.isCrossInstance === true

      if (isCrossInstanceSource && !hasPreferences) {
        return
      }

      queryClient.setQueryData<InstanceMetadata | undefined>(
        metadataQueryKey,
        previous => {
          // Treat omitted metadata arrays/maps as empty for regular instance responses.
          // Backend omitempty omits empty tags/categories, and we must clear stale cache values.
          const nextCategories = isCrossInstanceSource? (previous?.categories ?? {}): (source.categories ?? {})
          const nextTags = isCrossInstanceSource? (previous?.tags ?? []): (source.tags ?? [])
          const nextPreferences =
            hasPreferences && source.preferences !== undefined? (source.preferences as AppPreferences | undefined) ?? previous?.preferences: previous?.preferences

          const next: InstanceMetadata = {
            categories: nextCategories,
            tags: nextTags,
            preferences: nextPreferences,
          }

          return next
        }
      )

      if (hasPreferences && source.preferences !== undefined) {
        const nextPreferences = source.preferences as AppPreferences | undefined
        if (nextPreferences !== undefined) {
          queryClient.setQueryData<AppPreferences | undefined>(
            ["instance-preferences", instanceId],
            nextPreferences
          )
        }
      }
    },
    [instanceId, metadataQueryKey, queryClient]
  )

  const updateAppInfoCache = useCallback(
    (source?: Pick<TorrentResponse, "appInfo"> | null) => {
      if (!source?.appInfo) {
        return
      }

      queryClient.setQueryData<QBittorrentAppInfo | undefined>(appInfoQueryKey, source.appInfo)
    },
    [appInfoQueryKey, queryClient]
  )

  // Detect if this is cross-seed filtering based on expression content
  const isCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes("Hash ==") && filters?.expr?.includes("||")
  }, [filters?.expr])
  const useCrossInstanceEndpoint = isAllInstancesView || isCrossSeedFiltering

  const instanceIdsKey = useMemo(
    () => (instanceIds && instanceIds.length > 0 ? [...instanceIds].sort((left, right) => left - right).join(",") : ""),
    [instanceIds]
  )

  const streamQueryKey = useMemo(
    () => ["torrents-list", instanceId, instanceIdsKey, 0, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering] as const,
    [instanceId, instanceIdsKey, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering]
  )

  const { instances } = useInstances()
  const activeInstanceIds = useMemo(
    () => (instances ?? []).filter(current => current.isActive).map(current => current.id).filter(id => id > 0),
    [instances]
  )

  // Concrete member set for an aggregated (all-instances / cross-instance) stream:
  // an explicit subset selection when provided, otherwise all active instances.
  // The stream needs concrete ids; if none can be resolved we fall back to polling.
  const streamInstanceIds = useMemo(() => {
    if (!useCrossInstanceEndpoint) {
      return undefined
    }
    const base = instanceIds && instanceIds.length > 0 ? instanceIds : activeInstanceIds
    const filtered = Array.from(new Set(base.filter(id => id > 0)))
    return filtered.length > 0 ? filtered : undefined
  }, [useCrossInstanceEndpoint, instanceIds, activeInstanceIds])

  const streamInstanceIdsKey = useMemo(
    () => (streamInstanceIds ? [...streamInstanceIds].sort((a, b) => a - b).join(",") : ""),
    [streamInstanceIds]
  )

  // Single-instance views stream directly; aggregated views stream the cross-instance
  // endpoint once a concrete member set is known (otherwise fall back to polling below).
  const streamParams = useMemo(() => {
    if (!enabled) {
      return null
    }

    if (useCrossInstanceEndpoint) {
      // Cross-seed filtering encodes large `Hash == ... || ...` expressions in the
      // filters. The SSE subscription is sent as an EventSource GET URL, so those
      // expressions would risk request-line/proxy limits and reconnect churn; keep
      // cross-seed views on cross-instance polling. Only the all-instances view streams.
      if (isCrossSeedFiltering) {
        return null
      }
      if (!streamInstanceIds || streamInstanceIds.length === 0) {
        return null
      }
      return {
        instanceId: 0,
        instanceIds: streamInstanceIds,
        page: 0,
        limit: pageSize,
        sort,
        order,
        search: search || undefined,
        filters,
      }
    }

    return {
      instanceId,
      page: 0,
      limit: pageSize,
      sort,
      order,
      search: search || undefined,
      filters,
    }
    // streamInstanceIdsKey captures streamInstanceIds membership for memoization.
  }, [enabled, filters, instanceId, useCrossInstanceEndpoint, isCrossSeedFiltering, streamInstanceIds, streamInstanceIdsKey, order, pageSize, search, sort])

  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data) {
        return
      }
      // Normalize the streamed snapshot once at the boundary so every sink — the
      // query cache (read by the REST-processing effect below), the retained
      // snapshot, and the table rows — sees identical camelCase cross-instance
      // metadata. Feeding the raw snake_case payload to the cache would let the
      // effect overwrite the table with un-normalized rows on the next tick,
      // flickering the Instance column.
      const data = normalizeStreamedSnapshot(payload.data)
      setLastStreamSnapshot(data)
      updateAppInfoCache(data)
      updateMetadataCache(data)
      queryClient.setQueryData(streamQueryKey, data)

      if (useCrossInstanceEndpoint) {
        // Aggregated streams only ever deliver the first page of cross-instance
        // torrents. Merge it into the displayed list keyed on instanceId+hash so
        // pages the user paginated in via REST survive: a wholesale replace would
        // reset the unified view to page 0 on every snapshot, so it could never
        // scroll past the first page (issue #1983). Page 0 stays authoritative for
        // its own window. See mergeStreamedCrossInstanceFirstPage.
        setAllTorrents(prev => mergeStreamedCrossInstanceFirstPage(prev, data))

        if (typeof data.total === "number") {
          setLastKnownTotal(data.total)
        }
        if (currentPage === 0 && typeof data.hasMore === "boolean") {
          setHasLoadedAll(!data.hasMore)
        }
        return
      }

      setAllTorrents(prev => {
        const nextTorrents = data.torrents ?? []

        if (data.total === 0 || nextTorrents.length === 0) {
          return []
        }

        // Page 0 is authoritative for its window (a row it omits was deleted or moved
        // off page 0, so it must not be re-added); pagination-loaded later pages are
        // preserved. See mergeStreamedFirstPage.
        return mergeStreamedFirstPage(
          prev,
          nextTorrents,
          typeof data.total === "number" ? data.total : undefined
        )
      })

      if (typeof data.total === "number") {
        setLastKnownTotal(data.total)
      }

      if (currentPage === 0 && typeof data.hasMore === "boolean") {
        setHasLoadedAll(!data.hasMore)
      }
    },
    [currentPage, pageSize, queryClient, streamQueryKey, updateAppInfoCache, updateMetadataCache, useCrossInstanceEndpoint]
  )

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams) && !isHiddenDelayed,
    onMessage: handleStreamPayload,
  })

  const shouldDisablePolling = Boolean(streamParams) && streamState.connected && !streamState.error
  const preferCachedQuery = currentPage === 0 && shouldDisablePolling
  // Keep the REST query (initial fetch + fallback polling) enabled until the
  // stream is actually connected, not just until it errors. While the stream is
  // still connecting (e.g. behind a buffering reverse proxy that delays the init
  // event) streamState.error is null but no data is arriving, so gating on error
  // alone would disable REST entirely and the first page would never load.
  const queryEnabled =
    enabled &&
    (currentPage > 0 || !streamParams || !streamState.connected || Boolean(streamState.error))

  // Reset state when instanceId, filters, search, or sort changes
  // Use JSON.stringify to avoid resetting on every object reference change during polling
  const filterKey = JSON.stringify(filters)
  const searchKey = search || ""

  useEffect(() => {
    setCurrentPage(0)
    setAllTorrents([])
    setHasLoadedAll(false)
    setLastKnownTotal(0)
    setLastProcessedPage(-1)
    setLastStreamSnapshot(null)
  }, [instanceId, filterKey, searchKey, sort, order, instanceIdsKey])

  useEffect(() => {
    if (lastKnownTotal <= 0) {
      return
    }

    setHasLoadedAll(previous => {
      const next = allTorrents.length >= lastKnownTotal
      return previous === next ? previous : next
    })
  }, [allTorrents.length, lastKnownTotal])

  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching, isPlaceholderData } = useQuery<TorrentResponse>({
    queryKey: ["torrents-list", instanceId, instanceIdsKey, currentPage, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering],
    queryFn: () => {
      if (useCrossInstanceEndpoint) {
        return api.getCrossInstanceTorrents({
          page: currentPage,
          limit: pageSize,
          sort,
          order,
          search,
          filters,
          instanceIds,
        })
      }

      return api.getTorrents(instanceId, {
        page: currentPage,
        limit: pageSize,
        sort,
        order,
        search,
        filters,
        preferCached: preferCachedQuery,
      })
    },
    // Trust backend cache - it returns immediately with stale data if needed
    staleTime: 0, // Always check with backend (it decides if cache is fresh)
    gcTime: 300000, // Keep in React Query cache for 5 minutes for navigation
    // Reuse the previous page's data while the next page is loading so the UI doesn't flash empty state
    placeholderData: currentPage > 0 ? ((previousData) => previousData) : undefined,
    // Only poll the first page to get fresh data - don't poll pagination pages
    // Reduce polling frequency for cross-instance calls since they're more expensive.
    // When the SSE stream is connected we disable polling entirely on the first page.
    refetchInterval:
      currentPage === 0? (
        pollingEnabled && !shouldDisablePolling? (useCrossInstanceEndpoint ? 10000 : TORRENT_STREAM_POLL_INTERVAL_MS): false
      ): false,
    refetchIntervalInBackground, // Controls background polling behavior
    refetchOnWindowFocus: currentPage === 0 && pollingEnabled,
    enabled: queryEnabled,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: enabled && !isAllInstancesView })

  const activeData = useMemo(() => {
    if (shouldDisablePolling && lastStreamSnapshot) {
      return lastStreamSnapshot
    }

    return data ?? lastStreamSnapshot ?? null
  }, [data, lastStreamSnapshot, shouldDisablePolling])

  // Update torrents when data arrives or changes (including optimistic updates)
  useEffect(() => {
    // When filters/search/sort change we reset lastProcessedPage to -1. Skip placeholder
    // data in that window so we don't repopulate the table with stale results from the
    // previous query while the new request is in-flight.
    if (isPlaceholderData && (lastProcessedPage === -1 || currentPage === 0)) {
      return
    }

    if (currentPage > 0 && isFetching && isPlaceholderData) {
      return
    }

    if (!data) {
      return
    }

    updateAppInfoCache(data)
    updateMetadataCache(data)

    if (data.total !== undefined) {
      setLastKnownTotal(data.total)
    }

    // When the first page reports zero results, immediately clear the list so
    // downstream UIs don't render stale rows from the previous query.
    if (currentPage === 0 && data.total === 0) {
      setAllTorrents([])
      setHasLoadedAll(true)
      setLastProcessedPage(currentPage)
      setIsLoadingMore(false)
      return
    }

    // Handle both regular torrents and cross-instance torrents
    const torrentsData = data.isCrossInstance? (data.crossInstanceTorrents || data.cross_instance_torrents): data.torrents

    if (!torrentsData) {
      setIsLoadingMore(false)
      return
    }

    // Check if this is a new page load or data update for current page
    const isNewPageLoad = currentPage !== lastProcessedPage
    const isDataUpdate = !isNewPageLoad // Same page, but data changed (optimistic updates)

    // For first page or true data updates (optimistic updates from mutations)
    if (currentPage === 0 || (isDataUpdate && currentPage === 0)) {
      // First page OR data update (optimistic updates): replace all
      setAllTorrents(torrentsData)
      // Use backend's HasMore field for accurate pagination
      setHasLoadedAll(!data.hasMore)

      // Mark this page as processed
      if (isNewPageLoad) {
        setLastProcessedPage(currentPage)
      }
    } else if (isNewPageLoad && currentPage > 0) {
      // Mark this page as processed FIRST to prevent double processing
      setLastProcessedPage(currentPage)

      // Append to existing for pagination
      setAllTorrents(prev => {
        const updatedTorrents = [...prev, ...torrentsData]
        return updatedTorrents
      })

      // Use backend's HasMore field for accurate pagination
      if (!data.hasMore) {
        setHasLoadedAll(true)
      }
    }

    setIsLoadingMore(false)
  }, [data, currentPage, lastProcessedPage, isFetching, isPlaceholderData, updateAppInfoCache, updateMetadataCache])

  // Load more function for pagination - following TanStack Query best practices
  const loadMore = () => {
    const now = Date.now()

    // TanStack Query pattern: check hasNextPage && !isFetching before calling fetchNextPage
    // Our equivalent: check !hasLoadedAll && !(isLoadingMore || isFetching)
    if (hasLoadedAll) {
      return
    }

    if (isLoadingMore || isFetching) {
      return
    }

    // Enhanced throttling: 500ms for rapid scroll scenarios (up from 300ms)
    // This helps prevent race conditions during very fast scrolling
    if (now - lastRequestTime < 500) {
      return
    }

    setLastRequestTime(now)
    setIsLoadingMore(true)
    setCurrentPage(prev => prev + 1)
  }

  // Extract stats from response or calculate defaults
  const stats = useMemo(() => {
    const source = activeData ?? data

    if (source?.stats) {
      return {
        total: source.total || source.stats.total || 0,
        downloading: source.stats.downloading || 0,
        seeding: source.stats.seeding || 0,
        paused: source.stats.paused || 0,
        error: source.stats.error || 0,
        totalDownloadSpeed: source.stats.totalDownloadSpeed || 0,
        totalUploadSpeed: source.stats.totalUploadSpeed || 0,
        totalSize: source.stats.totalSize || 0,
      }
    }

    return {
      total: source?.total || 0,
      downloading: 0,
      seeding: 0,
      paused: 0,
      error: 0,
      totalDownloadSpeed: 0,
      totalUploadSpeed: 0,
      totalSize: source?.stats?.totalSize || 0,
    }
  }, [activeData, data])

  // Check if data is from cache or fresh (backend provides this info)
  const cacheMetadata = activeData?.cacheMetadata ?? data?.cacheMetadata
  const isCachedData = cacheMetadata?.source === "cache"
  const isStaleData = cacheMetadata?.isStale === true

  const isInitialStreamLoading =
    currentPage === 0 &&
    enabled &&
    Boolean(streamParams) &&
    !streamState.error &&
    !lastStreamSnapshot &&
    !data

  const effectiveIsLoading =
    currentPage === 0 ? (isInitialStreamLoading || (queryEnabled && isLoading)) : isLoading

  const effectiveIsFetching =
    currentPage === 0 ? (queryEnabled && isFetching) : isFetching

  // Use lastKnownTotal when loading more pages to prevent flickering
  const effectiveTotalCount =
    currentPage > 0 && typeof activeData?.total !== "number"? lastKnownTotal: activeData?.total ?? lastKnownTotal

  const responseUseSubcategories = activeData?.useSubcategories ?? activeData?.serverState?.use_subcategories ?? data?.useSubcategories ?? data?.serverState?.use_subcategories ?? false
  const supportsSubcategories = isAllInstancesView ? responseUseSubcategories : (capabilities?.supportsSubcategories ?? false)

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: activeData?.counts ?? data?.counts,
    appInfo: activeData?.appInfo ?? data?.appInfo ?? null,
    categories: activeData?.categories ?? data?.categories,
    tags: activeData?.tags ?? data?.tags,
    trackerHealthSupported: activeData?.trackerHealthSupported ?? data?.trackerHealthSupported ?? false,
    supportsTorrentCreation: isAllInstancesView ? false : capabilities?.supportsTorrentCreation ?? true,
    capabilities: isAllInstancesView ? undefined : capabilities,
    serverState: activeData?.serverState ?? data?.serverState ?? null,
    useSubcategories: isAllInstancesView? responseUseSubcategories: (supportsSubcategories ? responseUseSubcategories : false),
    isLoading: effectiveIsLoading,
    isFetching: effectiveIsFetching,
    isLoadingMore,
    hasLoadedAll,
    loadMore,
    // Cross-instance information
    isCrossInstance: data?.isCrossInstance ?? useCrossInstanceEndpoint,
    isCrossSeedFiltering,
    isAllInstancesView,
    isCrossInstanceEndpoint: useCrossInstanceEndpoint,
    // Metadata about data freshness
    isFreshData: !isCachedData || !isStaleData,
    isCachedData,
    isStaleData,
    cacheAge: cacheMetadata?.age,
    isStreaming: shouldDisablePolling,
    streamConnected: streamState.connected,
    streamError: streamState.error,
    streamMeta: streamState.lastMeta,
    streamRetrying: streamState.retrying,
    streamNextRetryAt: streamState.nextRetryAt,
    streamRetryAttempt: streamState.retryAttempt,
  }
}
