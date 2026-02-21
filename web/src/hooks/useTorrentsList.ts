/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import type {
  AppPreferences,
  QBittorrentAppInfo,
  Torrent,
  TorrentFilters,
  TorrentResponse,
  TorrentStreamPayload,
} from "@/types"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"

export const TORRENT_STREAM_POLL_INTERVAL_MS = 3000
export const TORRENT_STREAM_POLL_INTERVAL_SECONDS = Math.max(
  1,
  Math.round(TORRENT_STREAM_POLL_INTERVAL_MS / 1000)
)

interface UseTorrentsListOptions {
  enabled?: boolean
  pollingEnabled?: boolean
  refetchIntervalInBackground?: boolean
  search?: string
  filters?: TorrentFilters
  sort?: string
  order?: "asc" | "desc"
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
  } = options

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
          const nextCategories = isCrossInstanceSource
            ? (previous?.categories ?? {})
            : (source.categories ?? {})
          const nextTags = isCrossInstanceSource
            ? (previous?.tags ?? [])
            : (source.tags ?? [])
          const nextPreferences =
            hasPreferences && source.preferences !== undefined
              ? (source.preferences as AppPreferences | undefined) ?? previous?.preferences
              : previous?.preferences

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

  const streamQueryKey = useMemo(
    () => ["torrents-list", instanceId, 0, filters, search, sort, order] as const,
    [instanceId, filters, search, sort, order]
  )

  const isCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes("Hash ==") && filters?.expr?.includes("||")
  }, [filters?.expr])

  const streamParams = useMemo(() => {
    if (!enabled || isCrossSeedFiltering) {
      return null
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
  }, [enabled, filters, instanceId, isCrossSeedFiltering, order, pageSize, search, sort])

  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data) {
        return
      }
      setLastStreamSnapshot(payload.data)
      updateAppInfoCache(payload.data)
      updateMetadataCache(payload.data)
      queryClient.setQueryData(streamQueryKey, payload.data)
      setAllTorrents(prev => {
        const nextTorrents = payload.data?.torrents ?? []

        if (payload.data?.total === 0 || nextTorrents.length === 0) {
          return []
        }

        if (prev.length === 0) {
          return nextTorrents
        }

        const totalFromPayload =
          typeof payload.data?.total === "number" ? payload.data.total : undefined

        const pageFromMeta =
          typeof payload.meta?.page === "number" && payload.meta.page >= 0
            ? payload.meta.page
            : undefined
        const pageIndex = pageFromMeta ?? 0
        const pageStart = Math.max(0, pageIndex * pageSize)
        const pageEnd = pageStart + nextTorrents.length

        const seen = new Set(nextTorrents.map(torrent => torrent.hash))

        const leadingSliceEnd = Math.min(pageStart, prev.length)
        const leading = leadingSliceEnd > 0 ? prev.slice(0, leadingSliceEnd) : []
        const trailingStart = Math.min(pageEnd, prev.length)
        const trailing = trailingStart < prev.length ? prev.slice(trailingStart) : []
        const displacedSlice = prev.slice(pageStart, Math.min(pageEnd, prev.length))

        const dedupedLeading = leading.filter(torrent => !seen.has(torrent.hash))
        const dedupedDisplaced = displacedSlice.filter(torrent => !seen.has(torrent.hash))
        const dedupedTrailing = trailing.filter(torrent => !seen.has(torrent.hash))

        const merged = [...dedupedLeading, ...nextTorrents, ...dedupedDisplaced, ...dedupedTrailing]

        if (totalFromPayload !== undefined && merged.length > totalFromPayload) {
          return merged.slice(0, totalFromPayload)
        }

        return merged
      })

      if (typeof payload.data.total === "number") {
        setLastKnownTotal(payload.data.total)
      }

      if (currentPage === 0 && typeof payload.data.hasMore === "boolean") {
        setHasLoadedAll(!payload.data.hasMore)
      }
    },
    [currentPage, pageSize, queryClient, streamQueryKey, updateAppInfoCache, updateMetadataCache]
  )

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams),
    onMessage: handleStreamPayload,
  })

  const shouldDisablePolling = Boolean(streamParams) && streamState.connected && !streamState.error
  const preferCachedQuery = currentPage === 0 && shouldDisablePolling
  const queryEnabled =
    enabled &&
    (currentPage > 0 || Boolean(streamState.error) || !streamParams)

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
  }, [instanceId, filterKey, searchKey, sort, order])

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
    queryKey: ["torrents-list", instanceId, currentPage, filters, search, sort, order, isCrossSeedFiltering],
    queryFn: () => {
      if (isCrossSeedFiltering) {
        return api.getCrossInstanceTorrents({
          page: currentPage,
          limit: pageSize,
          sort,
          order,
          search,
          filters,
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
    refetchInterval:
      currentPage === 0
        ? (
            pollingEnabled
              ? (isCrossSeedFiltering ? 10000 : (shouldDisablePolling ? false : TORRENT_STREAM_POLL_INTERVAL_MS))
              : false
          )
        : false,
    refetchIntervalInBackground,
    refetchOnWindowFocus: currentPage === 0 && pollingEnabled,
    enabled: queryEnabled,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled })

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
    const torrentsData = data.isCrossInstance
      ? (data.crossInstanceTorrents || data.cross_instance_torrents)
      : data.torrents

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
    currentPage > 0 && typeof activeData?.total !== "number"
      ? lastKnownTotal
      : activeData?.total ?? lastKnownTotal

  const supportsSubcategories = capabilities?.supportsSubcategories ?? false

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: activeData?.counts ?? data?.counts,
    appInfo: activeData?.appInfo ?? data?.appInfo ?? null,
    categories: activeData?.categories ?? data?.categories,
    tags: activeData?.tags ?? data?.tags,
    supportsTorrentCreation: capabilities?.supportsTorrentCreation ?? true,
    capabilities,
    serverState: activeData?.serverState ?? data?.serverState ?? null,
    useSubcategories: supportsSubcategories
      ? (
          activeData?.useSubcategories ??
          activeData?.serverState?.use_subcategories ??
          data?.useSubcategories ??
          data?.serverState?.use_subcategories ??
          false
        )
      : false,
    isLoading: effectiveIsLoading,
    isFetching: effectiveIsFetching,
    isLoadingMore,
    hasLoadedAll,
    loadMore,
    // Cross-instance information
    isCrossInstance: data?.isCrossInstance ?? false,
    isCrossSeedFiltering,
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
