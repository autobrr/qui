import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Torrent } from '@/types'

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

// This hook uses the standard paginated API, not SyncMainData
// It's simpler and more reliable for the current implementation
export function useTorrentsList(
  instanceId: number,
  options: UseTorrentsListOptions = {}
) {
  const { enabled = true, search, filters } = options
  
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [currentPage, setCurrentPage] = useState(0)
  const pageSize = 500 // Load 500 at a time
  
  const [stats, setStats] = useState({
    total: 0,
    downloading: 0,
    seeding: 0,
    paused: 0,
    error: 0,
    totalDownloadSpeed: 0,
    totalUploadSpeed: 0,
  })
  
  // Reset state when instanceId changes (different instance = completely new data)
  useEffect(() => {
    setCurrentPage(0)
    setAllTorrents([])
    setHasLoadedAll(false)
    // Also reset stats to avoid showing stale data from previous instance
    setStats({
      total: 0,
      downloading: 0,
      seeding: 0,
      paused: 0,
      error: 0,
      totalDownloadSpeed: 0,
      totalUploadSpeed: 0,
    })
  }, [instanceId])
  
  // Reset pagination when filters or search change (same instance, different view)
  useEffect(() => {
    setCurrentPage(0)
    // Don't clear torrents - keep showing old data while fetching new
    // This provides a smoother experience (stale-while-revalidate pattern)
    setHasLoadedAll(false)
  }, [filters, search])
  
  // Initial load
  const { data: initialData, isLoading: initialLoading, isFetching } = useQuery({
    queryKey: ['torrents-list', instanceId, currentPage, filters, search],
    queryFn: () => api.getTorrents(instanceId, { 
      page: currentPage, 
      limit: pageSize,
      sort: 'addedOn',
      order: 'desc',
      search,
      filters
    }),
    staleTime: 1000, // 1 second - ensure data is considered stale quickly
    refetchInterval: 3000, // Poll every 3 seconds for more responsive updates
    refetchIntervalInBackground: false, // Don't poll when tab is not active
    enabled,
  })
  
  // Update torrents when data arrives or instanceId changes
  useEffect(() => {
    if (initialData?.torrents) {
      if (currentPage === 0) {
        // First page, replace all
        setAllTorrents(initialData.torrents)
      } else {
        // Append to existing
        setAllTorrents(prev => [...prev, ...initialData.torrents])
      }
      
      // Update stats - use the total from the API response
      if (initialData.stats) {
        setStats({
          total: initialData.total || initialData.stats.total,
          downloading: initialData.stats.downloading || 0,
          seeding: initialData.stats.seeding || 0,
          paused: initialData.stats.paused || 0,
          error: initialData.stats.error || 0,
          totalDownloadSpeed: initialData.stats.totalDownloadSpeed || 0,
          totalUploadSpeed: initialData.stats.totalUploadSpeed || 0,
        })
      } else if (initialData.total) {
        setStats(prev => ({
          ...prev,
          total: initialData.total
        }))
      }
      
      // Check if we've loaded all - compare current loaded count with total
      const totalLoaded = currentPage === 0 ? initialData.torrents.length : allTorrents.length + initialData.torrents.length
      if (totalLoaded >= (initialData.total || initialData.stats?.total || 0)) {
        setHasLoadedAll(true)
      }
      
      setIsLoadingMore(false)
    }
  }, [initialData, currentPage, pageSize, instanceId, filters, search]) // Added filters and search to dependencies
  
  // Load more function
  const loadMore = () => {
    if (!hasLoadedAll && !isLoadingMore) {
      setIsLoadingMore(true)
      setCurrentPage(prev => prev + 1)
    }
  }
  
  // Since search is now handled server-side, we don't need client-side filtering
  const filteredTorrents = allTorrents
  
  return {
    torrents: filteredTorrents,
    allTorrents,
    totalCount: initialData?.total ?? stats.total, // Use fresh data total if available
    stats,
    isLoading: initialLoading && currentPage === 0,
    isFetching, // Indicates background refetch is happening
    isLoadingMore,
    hasLoadedAll,
    loadMore,
    isFreshData: !!initialData, // Flag to indicate if we have fresh data
  }
}