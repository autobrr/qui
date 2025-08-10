import { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Torrent, TorrentResponse, ServerState } from '@/types'

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
  
  const [offset, setOffset] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const limit = 500 // Load 500 at a time (good for bandwidth efficiency)
  
  const [serverState, setServerState] = useState<ServerState | null>(null)
  
  // Fetch server state (user statistics) using sync endpoint
  const { data: syncData, error: syncError } = useQuery({
    queryKey: ['server-state', instanceId],
    queryFn: async () => {
      try {
        const data = await api.syncMainData(instanceId, 0)
        console.log('Full sync response:', data)
        // API returns server_state with underscore, not camelCase
        return (data as any).server_state || data.serverState || null
      } catch (error) {
        console.error('Error fetching sync data:', error)
        throw error
      }
    },
    staleTime: 30000, // 30 seconds
    refetchInterval: 30000, // Refetch every 30 seconds
    enabled: enabled && !!instanceId,
  })
  
  if (syncError) {
    console.error('Sync query error:', syncError)
  }
  
  // Update serverState when sync data changes
  useEffect(() => {
    if (syncData) {
      console.log('ServerState data received:', syncData)
      setServerState(syncData)
    }
  }, [syncData])
  
  // Reset state when instanceId, filters, or search change
  useEffect(() => {
    setOffset(0)
    setAllTorrents([])
    setHasLoadedAll(false)
  }, [instanceId, filters, search])
  
  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching } = useQuery<TorrentResponse>({
    queryKey: ['torrents-list', instanceId, offset, filters, search],
    queryFn: () => api.getTorrents(instanceId, { 
      offset, 
      limit,
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
      if (offset === 0) {
        // First load, replace all
        setAllTorrents(data.torrents)
      } else {
        // Append to existing for pagination
        setAllTorrents(prev => {
          // Avoid duplicates by filtering out existing hashes
          const existingHashes = new Set(prev.map((t: any) => t.hash))
          const newTorrents = data.torrents.filter((t: any) => !existingHashes.has(t.hash))
          return [...prev, ...newTorrents]
        })
      }
      
      setIsLoadingMore(false)
    }
  }, [data, offset, limit])

  // Separate effect to check if all data is loaded
  useEffect(() => {
    if (data?.torrents) {
      // Use the backend's hasMore flag if available, otherwise fall back to our logic
      if (data.hasMore !== undefined) {
        setHasLoadedAll(!data.hasMore)
      } else {
        // Fallback logic: check if we received fewer torrents than requested
        const receivedCount = data.torrents.length
        const isLastLoad = receivedCount < limit
        const totalFromServer = data.total || 0
        const currentlyLoaded = allTorrents.length
        
        setHasLoadedAll(isLastLoad || currentlyLoaded >= totalFromServer)
      }
    }
  }, [data, allTorrents.length, limit])
  
  // Load more function for pagination
  const loadMore = () => {
    if (!hasLoadedAll && !isLoadingMore && !isFetching) {
      setIsLoadingMore(true)
      setOffset((prev: number) => prev + limit)
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
    serverState, // Include server state with user statistics
    isLoading: isLoading && offset === 0,
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