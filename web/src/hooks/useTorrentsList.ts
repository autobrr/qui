/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { Torrent, TorrentResponse } from "@/types"

interface UseTorrentsListOptions {
  enabled?: boolean
  search?: string
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
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
  const pageSize = 300 // Load 300 at a time (backend default)

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
  }, [instanceId, filterKey, searchKey, sort, order])

  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching } = useQuery<TorrentResponse>({
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
    // Only poll the first page to get fresh data - don't poll pagination pages
    refetchInterval: currentPage === 0 ? 3000 : false,
    refetchIntervalInBackground: false, // Don't poll when tab is not active
    enabled,
  })

  // Update torrents when data arrives
  useEffect(() => {
    if (data?.torrents && currentPage !== lastProcessedPage) {
      // Mark this page as processed
      setLastProcessedPage(currentPage)

      // Update last known total whenever we get data
      if (data.total !== undefined) {
        setLastKnownTotal(data.total)
      }

      if (currentPage === 0) {
        // First page, replace all
        setAllTorrents(data.torrents)
        // Use backend's HasMore field for accurate pagination
        setHasLoadedAll(!data.hasMore)
      } else {
        // Append to existing for pagination
        setAllTorrents(prev => {
          const updatedTorrents = [...prev, ...data.torrents]

          // Use backend's HasMore field for accurate pagination
          if (!data.hasMore) {
            setHasLoadedAll(true)
          }

          return updatedTorrents
        })
      }

      setIsLoadingMore(false)
    }
  }, [data, currentPage, pageSize, lastProcessedPage])

  // Load more function for pagination
  const loadMore = () => {
    const now = Date.now()

    // Throttle requests to max one per 300ms (industry standard for infinite scroll)
    if (now - lastRequestTime < 300) {
      return
    }

    // Block if we're already loading or have loaded everything
    if (hasLoadedAll) {
      return
    }

    if (isLoadingMore || isFetching) {
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
    }
  }, [data])

  // Check if data is from cache or fresh (backend provides this info)
  const isCachedData = data?.cacheMetadata?.source === "cache"
  const isStaleData = data?.cacheMetadata?.isStale === true

  // Use lastKnownTotal when loading more pages to prevent flickering
  const effectiveTotalCount = currentPage > 0 && !data?.total ? lastKnownTotal : (data?.total ?? 0)

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: data?.counts,
    categories: data?.categories,
    tags: data?.tags,
    serverState: null, // Server state is fetched separately by Dashboard
    isLoading: isLoading && currentPage === 0,
    isFetching,
    isLoadingMore,
    hasLoadedAll,
    loadMore,
    // Metadata about data freshness
    isFreshData: !isCachedData || !isStaleData,
    isCachedData,
    isStaleData,
    cacheAge: data?.cacheMetadata?.age,
  }
}