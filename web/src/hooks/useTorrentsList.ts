/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { api } from "@/lib/api"
import { isAllInstancesScope } from "@/lib/instances"
import type { Torrent, TorrentFilters, TorrentResponse } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useEffect, useMemo, useState } from "react"

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
  const shouldEnableQuery = enabled
  const isAllInstancesView = isAllInstancesScope(instanceId)

  const [currentPage, setCurrentPage] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [lastRequestTime, setLastRequestTime] = useState(0)
  const [lastKnownTotal, setLastKnownTotal] = useState(0)
  const [lastProcessedPage, setLastProcessedPage] = useState(-1)
  const pageSize = 300 // Load 300 at a time (backend default)

  // Reset state when instanceId, filters, search, or sort changes
  // Use JSON.stringify to avoid resetting on every object reference change during polling
  const filterKey = JSON.stringify(filters)
  const searchKey = search || ""
  const instanceIdsKey = useMemo(
    () => (instanceIds && instanceIds.length > 0 ? [...instanceIds].sort((left, right) => left - right).join(",") : ""),
    [instanceIds]
  )

  useEffect(() => {
    setCurrentPage(0)
    setAllTorrents([])
    setHasLoadedAll(false)
    setLastKnownTotal(0)
    setLastProcessedPage(-1)
  }, [instanceId, filterKey, searchKey, sort, order, instanceIdsKey])

  // Detect if this is cross-seed filtering based on expression content
  const isCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes('Hash ==') && filters?.expr?.includes('||')
  }, [filters?.expr])
  const useCrossInstanceEndpoint = isAllInstancesView || isCrossSeedFiltering

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
      })
    },
    // Trust backend cache - it returns immediately with stale data if needed
    staleTime: 0, // Always check with backend (it decides if cache is fresh)
    gcTime: 300000, // Keep in React Query cache for 5 minutes for navigation
    // Reuse the previous page's data while the next page is loading so the UI doesn't flash empty state
    placeholderData: currentPage > 0 ? ((previousData) => previousData) : undefined,
    // Only poll the first page to get fresh data - don't poll pagination pages
    // Reduce polling frequency for cross-instance calls since they're more expensive
    refetchInterval: currentPage === 0
      ? (pollingEnabled ? (useCrossInstanceEndpoint ? 10000 : 3000) : false)
      : false,
    refetchIntervalInBackground, // Controls background polling behavior
    refetchOnWindowFocus: currentPage === 0,
    enabled: shouldEnableQuery,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: shouldEnableQuery && !isAllInstancesView })

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
    if (data?.stats) {
      return {
        total: data.total || data.stats.total || 0,
        downloading: data.stats.downloading || 0,
        seeding: data.stats.seeding || 0,
        paused: data.stats.paused || 0,
        error: data.stats.error || 0,
        totalDownloadSpeed: data.stats.totalDownloadSpeed || 0,
        totalUploadSpeed: data.stats.totalUploadSpeed || 0,
        totalSize: data.stats.totalSize || 0,
      }
    }

    return {
      total: data?.total || 0,
      downloading: 0,
      seeding: 0,
      paused: 0,
      error: 0,
      totalDownloadSpeed: 0,
      totalUploadSpeed: 0,
      totalSize: data?.stats?.totalSize || 0,
    }
  }, [data])

  // Check if data is from cache or fresh (backend provides this info)
  const isCachedData = data?.cacheMetadata?.source === "cache"
  const isStaleData = data?.cacheMetadata?.isStale === true

  // Use lastKnownTotal when loading more pages to prevent flickering
  const effectiveTotalCount = currentPage > 0 && !data?.total ? lastKnownTotal : (data?.total ?? 0)

  const responseUseSubcategories = data?.useSubcategories ?? data?.serverState?.use_subcategories ?? false
  const supportsSubcategories = isAllInstancesView
    ? responseUseSubcategories
    : (capabilities?.supportsSubcategories ?? false)

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: data?.counts,
    categories: data?.categories,
    tags: data?.tags,
    supportsTorrentCreation: isAllInstancesView ? false : capabilities?.supportsTorrentCreation ?? true,
    capabilities: isAllInstancesView ? undefined : capabilities,
    serverState: data?.serverState ?? null,
    useSubcategories: isAllInstancesView
      ? responseUseSubcategories
      : (supportsSubcategories ? responseUseSubcategories : false),
    isLoading: isLoading && currentPage === 0,
    isFetching,
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
    cacheAge: data?.cacheMetadata?.age,
  }
}
