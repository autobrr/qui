/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { api } from "@/lib/api"
import { useSyncStream } from "@/contexts/SyncStreamContext"
import type { Torrent, TorrentFilters, TorrentResponse, TorrentStreamPayload } from "@/types"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"

export const TORRENT_STREAM_POLL_INTERVAL_MS = 3000
export const TORRENT_STREAM_POLL_INTERVAL_SECONDS = Math.max(
  1,
  Math.round(TORRENT_STREAM_POLL_INTERVAL_MS / 1000)
)

interface UseTorrentsListOptions {
  enabled?: boolean
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
  const { enabled = true, search, filters, sort = "added_on", order = "desc" } = options

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

  const streamQueryKey = useMemo(
    () => ["torrents-list", instanceId, 0, filters, search, sort, order] as const,
    [instanceId, filters, search, sort, order]
  )

  const streamParams = useMemo(() => {
    if (!enabled) {
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
  }, [enabled, instanceId, pageSize, sort, order, search, filters])

  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data) {
        return
      }
      setLastStreamSnapshot(payload.data)
      queryClient.setQueryData(streamQueryKey, payload.data)
      setAllTorrents(prev => {
        const nextTorrents = payload.data?.torrents ?? []

        if (payload.data?.total === 0 || nextTorrents.length === 0) {
          return []
        }

        if (prev.length === 0) {
          return nextTorrents
        }

        const seen = new Set(nextTorrents.map(torrent => torrent.hash))
        const merged = [...nextTorrents]
        const totalFromPayload =
          typeof payload.data?.total === "number" ? payload.data.total : undefined
        let remainingSlots =
          totalFromPayload !== undefined ? Math.max(0, totalFromPayload - nextTorrents.length) : undefined

        for (const torrent of prev) {
          if (!seen.has(torrent.hash)) {
            if (remainingSlots !== undefined) {
              if (remainingSlots === 0) {
                break
              }
              remainingSlots -= 1
            }
            merged.push(torrent)
          }
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
    [currentPage, queryClient, streamQueryKey]
  )

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams),
    onMessage: handleStreamPayload,
  })

  const [httpFallbackAllowed, setHttpFallbackAllowed] = useState(() => !streamParams)

  useEffect(() => {
    if (!enabled || !streamParams) {
      setHttpFallbackAllowed(true)
      return
    }

    if (streamState.error) {
      setHttpFallbackAllowed(true)
      return
    }

    if (streamState.connected) {
      setHttpFallbackAllowed(false)
      return
    }

    setHttpFallbackAllowed(false)

    if (typeof window === "undefined") {
      return
    }

    const timeoutId = window.setTimeout(() => {
      setHttpFallbackAllowed(true)
    }, TORRENT_STREAM_POLL_INTERVAL_MS)

    return () => {
      window.clearTimeout(timeoutId)
    }
  }, [enabled, streamParams, streamState.connected, streamState.error])

  const shouldDisablePolling = Boolean(streamParams) && streamState.connected && !streamState.error
  const queryEnabled =
    enabled &&
    (currentPage > 0 || Boolean(streamState.error) || (!streamState.connected && httpFallbackAllowed))

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

  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching, isPlaceholderData } = useQuery<TorrentResponse>({
    queryKey: ["torrents-list", instanceId, currentPage, filters, search, sort, order],
    queryFn: () => {
      return api.getTorrents(instanceId, {
        page: currentPage,
        limit: pageSize,
        sort,
        order,
        search,
        filters,
      })
    },
    // Trust backend cache - it returns immediately with stale data if needed
    staleTime: 0, // Always check with backend (it decides if cache is fresh)
    gcTime: 300000, // Keep in React Query cache for 5 minutes for navigation
    // Reuse the previous page's data while the next page is loading so the UI doesn't flash empty state
    placeholderData: currentPage > 0 ? ((previousData) => previousData) : undefined,
    // Only poll the first page to get fresh data - don't poll pagination pages
    refetchInterval:
      currentPage === 0 ? (shouldDisablePolling ? false : TORRENT_STREAM_POLL_INTERVAL_MS) : false,
    refetchIntervalInBackground: false, // Don't poll when tab is not active
    enabled: queryEnabled,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled })

  const activeData = useMemo(() => {
    if (shouldDisablePolling && lastStreamSnapshot) {
      return lastStreamSnapshot
    }

    if (currentPage === 0) {
      return data ?? lastStreamSnapshot ?? null
    }

    return lastStreamSnapshot ?? data ?? null
  }, [currentPage, data, lastStreamSnapshot, shouldDisablePolling])

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

    if (!data.torrents) {
      setIsLoadingMore(false)
      return
    }

    // Check if this is a new page load or data update for current page
    const isNewPageLoad = currentPage !== lastProcessedPage
    const isDataUpdate = !isNewPageLoad // Same page, but data changed (optimistic updates)

    // For first page or true data updates (optimistic updates from mutations)
    if (currentPage === 0 || (isDataUpdate && currentPage === 0)) {
      // First page OR data update (optimistic updates): replace all
      setAllTorrents(data.torrents)
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
        const updatedTorrents = [...prev, ...data.torrents]
        return updatedTorrents
      })

      // Use backend's HasMore field for accurate pagination
      if (!data.hasMore) {
        setHasLoadedAll(true)
      }
    }

    setIsLoadingMore(false)
  }, [data, currentPage, lastProcessedPage, isFetching, isPlaceholderData])

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
    isLoading: isLoading && currentPage === 0,
    isFetching,
    isLoadingMore,
    hasLoadedAll,
    loadMore,
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
