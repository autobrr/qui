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

// Simplified hook that trusts the backend's stale-while-revalidate pattern
// Backend handles all caching complexity and returns fresh or stale data immediately
export function useTorrentsList(
  instanceId: number,
  options: UseTorrentsListOptions = {}
) {
  const { enabled = true, search, filters, sort = "added_on", order = "desc" } = options

  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])

  // Reset state when instanceId, filters, search, or sort changes
  // Use JSON.stringify to avoid resetting on every object reference change during polling
  const filterKey = JSON.stringify(filters)
  const searchKey = search || ""

  useEffect(() => {
    setAllTorrents([])
  }, [instanceId, filterKey, searchKey, sort, order])

  // Query for torrents - backend returns complete dataset
  const { data, isLoading, isFetching } = useQuery<TorrentResponse>({
    queryKey: ["torrents-list", instanceId, filters, search, sort, order],
    queryFn: () => api.getTorrents(instanceId, {
      page: 0,
      limit: 0, // Backend ignores this and returns all data
      sort,
      order,
      search,
      filters,
    }),
    staleTime: 0, // Always check with backend
    gcTime: 300000, // Keep in React Query cache for 5 minutes
    refetchInterval: 3000, // Poll for updates every 3 seconds
    refetchIntervalInBackground: false,
    enabled,
  })

  // Update torrents when data arrives
  useEffect(() => {
    if (data?.torrents) {
      setAllTorrents(data.torrents)
    }
  }, [data])

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

  return {
    torrents: allTorrents,
    totalCount: data?.total ?? 0,
    stats,
    counts: data?.counts,
    categories: data?.categories,
    tags: data?.tags,
    serverState: null, // Server state is fetched separately by Dashboard
    isLoading,
    isFetching,
    // Metadata about data freshness
    isFreshData: !isCachedData || !isStaleData,
    isCachedData,
    isStaleData,
    cacheAge: data?.cacheMetadata?.age,
  }
}