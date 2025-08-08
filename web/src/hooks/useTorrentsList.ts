import { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Torrent, TorrentResponse } from '@/types'

interface UseTorrentsListOptions {
  enabled?: boolean
  search?: string
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
}

// Simplified hook that trusts the backend's stale-while-revalidate pattern
// Backend handles all caching complexity and returns fresh or stale data immediately
export function useTorrentsList(
  instanceId: number,
  options: UseTorrentsListOptions = {}
) {
  const { enabled = true, search, filters } = options
  
  const [currentPage, setCurrentPage] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const pageSize = 500 // Load 500 at a time (backend default)
  
  // Reset state when instanceId, filters, or search change
  useEffect(() => {
    setCurrentPage(0)
    setAllTorrents([])
    setHasLoadedAll(false)
  }, [instanceId, filters, search])
  
  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching } = useQuery<TorrentResponse>({
    queryKey: ['torrents-list', instanceId, currentPage, filters, search],
    queryFn: () => api.getTorrents(instanceId, { 
      page: currentPage, 
      limit: pageSize,
      sort: 'addedOn',
      order: 'desc',
      search,
      filters
    }),
    // Trust backend cache - it returns immediately with stale data if needed
    staleTime: 0, // Always check with backend (it decides if cache is fresh)
    gcTime: 300000, // Keep in React Query cache for 5 minutes for navigation
    refetchInterval: 3000, // Poll every 3 seconds to trigger backend's stale check
    refetchIntervalInBackground: false, // Don't poll when tab is not active
    enabled,
  })
  
  // Update torrents when data arrives
  useEffect(() => {
    if (data?.torrents) {
      if (currentPage === 0) {
        // First page, replace all
        setAllTorrents(data.torrents)
      } else {
        // Append to existing for pagination
        setAllTorrents(prev => {
          // Avoid duplicates by filtering out existing hashes
          const existingHashes = new Set(prev.map(t => t.hash))
          const newTorrents = data.torrents.filter(t => !existingHashes.has(t.hash))
          return [...prev, ...newTorrents]
        })
      }
      
      // Check if we've loaded all torrents
      const totalLoaded = currentPage === 0 
        ? data.torrents.length 
        : allTorrents.length + data.torrents.length
      
      if (totalLoaded >= (data.total || 0) || data.torrents.length < pageSize) {
        setHasLoadedAll(true)
      }
      
      setIsLoadingMore(false)
    }
  }, [data, currentPage, pageSize])
  
  // Load more function for pagination
  const loadMore = () => {
    if (!hasLoadedAll && !isLoadingMore && !isFetching) {
      setIsLoadingMore(true)
      setCurrentPage(prev => prev + 1)
    }
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
  const isCachedData = data?.cacheMetadata?.source === 'cache'
  const isStaleData = data?.cacheMetadata?.isStale === true
  
  return {
    torrents: allTorrents,
    totalCount: data?.total ?? 0,
    stats,
    counts: data?.counts, // Return counts from backend
    categories: data?.categories, // Return categories from backend
    tags: data?.tags, // Return tags from backend
    isLoading: isLoading && currentPage === 0,
    isFetching, // True when React Query is fetching (but we may have stale data)
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