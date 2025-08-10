/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState, useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Torrent, MainData } from '@/types'

interface UseTorrentsSyncOptions {
  enabled?: boolean
  pollingInterval?: number
  search?: string
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
}

export function useTorrentsSync(
  instanceId: number,
  options: UseTorrentsSyncOptions = {}
) {
  const { enabled = true, pollingInterval = 2000, search, filters } = options
  
  const [mainData, setMainData] = useState<MainData | null>(null)
  const [torrents, setTorrents] = useState<Torrent[]>([])
  const [filteredTorrents, setFilteredTorrents] = useState<Torrent[]>([])
  const ridRef = useRef(0)
  
  const [stats, setStats] = useState({
    total: 0,
    downloading: 0,
    seeding: 0,
    paused: 0,
    error: 0,
    totalDownloadSpeed: 0,
    totalUploadSpeed: 0,
  })
  
  // Initial load - get first batch of torrents
  const { data: initialData, isLoading: initialLoading } = useQuery({
    queryKey: ['torrents-initial', instanceId],
    queryFn: () => api.getTorrents(instanceId, { 
      page: 0, 
      limit: 200, // Get more initially for better UX
      sort: 'added_on',
      order: 'desc'
    }),
    staleTime: Infinity, // Never refetch initial data
    enabled,
  })
  
  // Set initial torrents
  useEffect(() => {
    if (initialData?.torrents) {
      setTorrents(initialData.torrents)
      if (initialData.stats) {
        setStats(prev => ({
          ...prev,
          ...initialData.stats,
        }))
      }
    }
  }, [initialData])
  
  // Real-time sync updates using SyncMainData
  useEffect(() => {
    if (!enabled || initialLoading || !initialData) return
    
    const syncInterval = setInterval(async () => {
      try {
        const updates = await api.syncMainData(instanceId, ridRef.current)
        
        if (updates.fullUpdate || ridRef.current === 0) {
          // Full update - replace all torrents
          const newTorrents = Object.values(updates.torrents || {})
          setTorrents(newTorrents)
          setMainData(updates)
        } else {
          // Incremental update
          setTorrents(prev => {
            const updated = [...prev]
            
            // Update existing torrents
            if (updates.torrents) {
              Object.entries(updates.torrents).forEach(([hash, torrent]) => {
                const index = updated.findIndex(t => t.hash === hash)
                if (index >= 0) {
                  updated[index] = { ...updated[index], ...torrent }
                } else {
                  // New torrent
                  updated.push(torrent)
                }
              })
            }
            
            // Remove deleted torrents
            if (updates.torrentsRemoved) {
              return updated.filter(t => !updates.torrentsRemoved?.includes(t.hash))
            }
            
            return updated
          })
          
          setMainData(prev => ({
            ...prev,
            ...updates,
            torrents: { ...prev?.torrents, ...updates.torrents },
          }))
        }
        
        // Update stats from server state
        if (updates.serverState) {
          setStats(prev => ({
            ...prev,
            totalDownloadSpeed: updates.serverState?.dl_info_speed || 0,
            totalUploadSpeed: updates.serverState?.up_info_speed || 0,
          }))
        }
        
        ridRef.current = updates.rid
      } catch (error) {
        console.error('Sync error:', error)
      }
    }, pollingInterval)
    
    return () => clearInterval(syncInterval)
  }, [instanceId, enabled, pollingInterval, initialLoading, initialData])
  
  // Client-side filtering
  useEffect(() => {
    let result = [...torrents]
    
    // Search filter
    if (search) {
      const searchLower = search.toLowerCase()
      result = result.filter(t => {
        const nameMatch = t.name.toLowerCase().includes(searchLower)
        const categoryMatch = t.category?.toLowerCase().includes(searchLower)
        
        // Handle tags which could be string[] or string from API
        let tagsMatch = false
        if (Array.isArray(t.tags)) {
          tagsMatch = t.tags.some(tag => tag.toLowerCase().includes(searchLower))
        } else if (typeof t.tags === 'string') {
          tagsMatch = (t.tags as any).toLowerCase().includes(searchLower)
        }
        
        return nameMatch || categoryMatch || tagsMatch
      })
    }
    
    // Status filter
    if (filters?.status?.length) {
      result = result.filter(t => filters.status.includes(t.state))
    }
    
    // Category filter
    if (filters?.categories?.length) {
      result = result.filter(t => t.category && filters.categories.includes(t.category))
    }
    
    // Tags filter
    if (filters?.tags?.length) {
      result = result.filter(t => {
        if (!t.tags) return false
        
        // Handle tags which could be string[] or comma-separated string from API
        let torrentTags: string[] = []
        if (Array.isArray(t.tags)) {
          torrentTags = t.tags
        } else if (typeof t.tags === 'string') {
          torrentTags = (t.tags as any).split(',').map((tag: string) => tag.trim())
        }
        
        return filters.tags.some(tag => torrentTags.includes(tag))
      })
    }
    
    // Sort by added date (newest first)
    result.sort((a, b) => (b.added_on || 0) - (a.added_on || 0))
    
    setFilteredTorrents(result)
    
    // Update stats based on filtered torrents
    const filteredStats = {
      total: result.length,
      downloading: result.filter(t => t.state === 'downloading' || t.state === 'stalledDL').length,
      seeding: result.filter(t => t.state === 'uploading' || t.state === 'stalledUP').length,
      paused: result.filter(t => t.state === 'pausedDL' || t.state === 'pausedUP').length,
      error: result.filter(t => t.state === 'error' || t.state === 'missingFiles').length,
    }
    
    setStats(prev => ({
      ...prev,
      ...filteredStats,
    }))
  }, [torrents, search, filters])
  
  return {
    torrents: filteredTorrents,
    allTorrents: torrents,
    totalCount: filteredTorrents.length,
    stats,
    isLoading: initialLoading,
    mainData,
  }
}