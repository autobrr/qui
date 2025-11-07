/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { Instance, Torrent, TorrentFile } from "@/types"
import { useQuery, useQueries } from "@tanstack/react-query"
import { useMemo } from "react"

// Cross-seed matching utilities
export const normalizePath = (path: string) => path?.toLowerCase().replace(/[\\\/]+/g, '/').replace(/\/$/, '') || ''
export const normalizeName = (name: string) => name?.toLowerCase().trim() || ''

export const getBaseFileName = (path: string): string => {
  const normalized = path.replace(/\\/g, '/').trim()
  const parts = normalized.split('/')
  return parts[parts.length - 1].toLowerCase()
}

export const normalizeFileName = (name: string): string => {
  return name.toLowerCase()
    .replace(/\.(mkv|mp4|avi|mov|wmv|flv|webm|m4v|mpg|mpeg)$/i, '') // Remove extension
    .replace(/[._\-\s]+/g, '') // Remove separators
}

export const calculateSimilarity = (str1: string, str2: string): number => {
  if (str1 === str2) return 1.0
  if (!str1 || !str2) return 0
  
  // Use the longer string as reference
  const longer = str1.length >= str2.length ? str1 : str2
  const shorter = str1.length < str2.length ? str1 : str2
  
  // If shorter string is contained in longer, high similarity
  if (longer.includes(shorter)) {
    return shorter.length / longer.length
  }
  
  // Calculate Levenshtein distance
  const matrix: number[][] = []
  for (let i = 0; i <= longer.length; i++) {
    matrix[i] = [i]
  }
  for (let j = 0; j <= shorter.length; j++) {
    matrix[0][j] = j
  }
  
  for (let i = 1; i <= longer.length; i++) {
    for (let j = 1; j <= shorter.length; j++) {
      if (longer[i - 1] === shorter[j - 1]) {
        matrix[i][j] = matrix[i - 1][j - 1]
      } else {
        matrix[i][j] = Math.min(
          matrix[i - 1][j - 1] + 1, // substitution
          matrix[i][j - 1] + 1,     // insertion
          matrix[i - 1][j] + 1      // deletion
        )
      }
    }
  }
  
  const distance = matrix[longer.length][shorter.length]
  return 1 - (distance / longer.length)
}

// Extended torrent type with cross-seed metadata
export interface CrossSeedTorrent extends Torrent {
  instanceId: number
  instanceName: string
  matchType: 'infohash' | 'content_path' | 'save_path' | 'name'
}

// Search for potential cross-seed matches
export const searchCrossSeedMatches = async (
  torrent: Torrent,
  instance: Instance,
  currentInstanceId: number,
  currentFiles: TorrentFile[] = [],
  resolvedInfohashV1?: string,
  resolvedInfohashV2?: string
): Promise<CrossSeedTorrent[]> => {
  const executionId = Math.random().toString(36).substring(7)
  
  try {
    // Strategy: Make multiple targeted searches to find matches efficiently
    const allMatches: Torrent[] = []
    
    // Extract a distinctive search term from the torrent name
    let searchName = torrent.name
    const lastDot = torrent.name.lastIndexOf('.')
    if (lastDot > 0 && lastDot > torrent.name.lastIndexOf('/')) {
      // Has extension and it's not in a folder name
      const extension = torrent.name.slice(lastDot + 1)
      // Only strip if it looks like a real extension (2-5 chars, alphanumeric)
      if (extension.length >= 2 && extension.length <= 5 && /^[a-z0-9]+$/i.test(extension)) {
        searchName = torrent.name.slice(0, lastDot)
      }
    }
    
    // Remove metadata patterns to extract core content name
    let cleanedName = searchName
      .replace(/\(\d{4}\)/g, '') // Remove years like (2025)
      .replace(/\[[^\]]+\]/g, '') // Remove brackets like [WEB FLAC]
      .replace(/\{[^}]+\}/g, '') // Remove braces
      .trim()
    
    // Strip everything from special characters onwards (: etc) to avoid search issues
    cleanedName = cleanedName.split(/[꞉:]/)[0].trim()
    
    // Use the cleaned name for search (without metadata but with full artist/title)
    const searchTerm = cleanedName || searchName
    const nameSearchResponse = await api.getTorrents(instance.id, {
      search: searchTerm,
      limit: 2000,
    })
    
    const nameTorrents = nameSearchResponse.torrents || []

    // Add name search results
    allMatches.push(...nameTorrents)
    
    // If we have info hashes, also search by them
    if (resolvedInfohashV1 || resolvedInfohashV2) {
      const hashToSearch = resolvedInfohashV1 || resolvedInfohashV2
      const hashSearchResponse = await api.getTorrents(instance.id, {
        search: hashToSearch,
        limit: 2000,
      })
      
      const hashTorrents = hashSearchResponse.torrents || []

      // Merge results, avoiding duplicates
      let newHashMatches = 0
      for (const t of hashTorrents) {
        if (!allMatches.some(m => m.hash === t.hash)) {
          allMatches.push(t)
          newHashMatches++
        }
      }
    }

    // Normalize strings for comparison
    const normalizedContentPath = normalizePath(torrent.content_path || '')
    const normalizedName = normalizeName(torrent.name)
    
    // Filter matching torrents with different matching strategies
    const matches = allMatches.filter((t: Torrent) => {
      // Exclude the exact current torrent (same instance AND same hash)
      if (instance.id === currentInstanceId && t.hash === torrent.hash) {
        return false
      }
      
      // Strategy 1: Exact info hash match (cross-seeding same torrent)
      if ((resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) || 
          (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2)) {
        return true
      }
      
      // Strategy 2: Same content path (same files, different torrent)
      if (normalizedContentPath && t.content_path) {
        const otherContentPath = normalizePath(t.content_path)
        if (otherContentPath === normalizedContentPath) {
          return true
        }
      }
      
      // Strategy 3: Same torrent name (likely same content)
      if (normalizedName && t.name) {
        const otherName = normalizeName(t.name)
        if (otherName === normalizedName) {
          return true
        }
      }
      
      // Strategy 4: Similar save path (for single-file torrents)
      if (torrent.save_path && t.save_path) {
        const normalizedCurrentSavePath = normalizePath(torrent.save_path)
        const otherSavePath = normalizePath(t.save_path)
        if (normalizedCurrentSavePath && otherSavePath === normalizedCurrentSavePath) {
          // Also check if file names match for single files
          const currentBaseName = normalizedName.split('/').pop() || ''
          const otherBaseName = normalizeName(t.name).split('/').pop() || ''
          if (currentBaseName === otherBaseName) {
            return true
          }
        }
      }
      
      // Strategy 5: Fuzzy file name matching with similarity threshold
      const currentBaseFile = getBaseFileName(torrent.name)
      const otherBaseFile = getBaseFileName(t.name)
      
      // Try base file comparison (handles folder/file.mkv scenarios)
      if (currentBaseFile && otherBaseFile) {
        const currentNormalized = normalizeFileName(currentBaseFile)
        const otherNormalized = normalizeFileName(otherBaseFile)
        
        const similarity = calculateSimilarity(currentNormalized, otherNormalized)

        // If similarity is high (>= 90%), consider it a potential match
        if (similarity >= 0.9 && currentNormalized.length > 0) {
          return true
        }
      }
      
      // Also try full name comparison with normalization
      const currentFullNormalized = normalizeFileName(torrent.name)
      const otherFullNormalized = normalizeFileName(t.name)
      const fullSimilarity = calculateSimilarity(currentFullNormalized, otherFullNormalized)

      if (fullSimilarity >= 0.9 && currentFullNormalized.length > 0) {
        return true
      }
      return false
    })

    // Concurrency-limited approach to prevent spawning hundreds of simultaneous requests
    const MAX_CONCURRENT_REQUESTS = 4
    const deepMatchResults: { torrent: Torrent; isMatch: boolean; matchType: string }[] = []
    
    // Process matches in batches with concurrency control
    for (let i = 0; i < matches.length; i += MAX_CONCURRENT_REQUESTS) {
      const batch = matches.slice(i, i + MAX_CONCURRENT_REQUESTS)
      const batchResults = await Promise.all(
        batch.map(async (t: Torrent) => {
          // Skip deep matching if we already have strong matches (info hash, content path, or torrent name)
          const hasStrongMatch = 
            (resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) ||
            (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2) ||
            (normalizedContentPath && normalizePath(t.content_path) === normalizedContentPath) ||
            (normalizedName && normalizeName(t.name) === normalizedName)
          
          if (hasStrongMatch) {
            return { torrent: t, isMatch: true, matchType: 'strong' }
          }
          
          // If we have files, do deep comparison
          if (currentFiles.length > 0) {
            try {
              const otherFiles = await api.getTorrentFiles(instance.id, t.hash)

              // Compare file structures
              const currentFileSet = new Set(
                currentFiles.map(f => ({
                  name: normalizeFileName(getBaseFileName(f.name)),
                  size: f.size
                })).map(f => `${f.name}:${f.size}`)
              )
              
              const otherFileSet = new Set(
                otherFiles.map(f => ({
                  name: normalizeFileName(getBaseFileName(f.name)),
                  size: f.size
                })).map(f => `${f.name}:${f.size}`)
              )
              
              // Check overlap - if significant overlap, it's a match
              const intersection = new Set([...currentFileSet].filter(x => otherFileSet.has(x)))
              const overlapPercent = intersection.size / Math.max(currentFileSet.size, otherFileSet.size)

              if (overlapPercent > 0.8) { // 80% of files match
                return { torrent: t, isMatch: true, matchType: 'file_content' }
              } else {
              }
            } catch (err) {
              console.log(`[CrossSeed] Deep match: "${t.name}" - ⚠️  Could not fetch files for deep matching:`, err)
            }
          }

          // Keep weak matches (name, path) without deep verification
          return { torrent: t, isMatch: true, matchType: 'weak' }
        })
      )
      
      deepMatchResults.push(...batchResults)
    }

    const finalMatches = deepMatchResults.filter(r => r.isMatch).map(r => r.torrent)
    const enrichedMatches = finalMatches.map((t: Torrent): CrossSeedTorrent => {
      // Determine match type for display
      let matchType: CrossSeedTorrent['matchType'] = 'name'
      if ((resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) || 
          (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2)) {
        matchType = 'infohash'
      } else if (normalizedContentPath && normalizePath(t.content_path) === normalizedContentPath) {
        matchType = 'content_path'
      } else if (torrent.save_path && normalizePath(t.save_path) === normalizePath(torrent.save_path)) {
        matchType = 'save_path'
      } else {
      }
      
      return { ...t, instanceId: instance.id, instanceName: instance.name, matchType }
    })

    return enrichedMatches
  } catch (error) {
    console.error(`[CrossSeed] ❌ ERROR in query [ID: ${executionId}] for instance ${instance.name}:`, error)
    throw error
  }
}

// Hook to find cross-seed matches for a torrent
export const useCrossSeedMatches = (
  instanceId: number,
  torrent: Torrent | null,
  enabled: boolean = true
) => {
  // Determine if torrent is disc content (skip file fetching for disc content)
  const isDiscContent = useMemo(() => {
    if (!torrent?.content_path) return false
    const contentPath = torrent.content_path.toLowerCase()
    return contentPath.includes('video_ts') || contentPath.includes('bdmv')
  }, [torrent?.content_path])

  // Get resolved info hashes
  const resolvedInfohashV1 = torrent?.infohash_v1 || torrent?.hash
  const resolvedInfohashV2 = torrent?.infohash_v2

  // Fetch current torrent's files for deep matching (skip for disc content)
  const { data: currentTorrentFiles, isLoading: isLoadingCurrentFiles } = useQuery({
    queryKey: ["torrent-files-crossseed", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentFiles(instanceId, torrent!.hash),
    enabled: enabled && !!torrent && !isDiscContent,
    staleTime: 60000,
    gcTime: 5 * 60 * 1000,
  })

  // Fetch all instances for cross-seed matching
  const { data: allInstances, isLoading: isLoadingInstances } = useQuery({
    queryKey: ["instances"],
    queryFn: api.getInstances,
    enabled,
    staleTime: 60000,
  })

  // Create stable instance IDs and files key for dependencies
  const instanceIds = useMemo(
    () => allInstances?.map(i => i.id).sort().join(',') || '',
    [allInstances]
  )

  const currentFilesKey = useMemo(
    () => currentTorrentFiles?.map(f => `${f.name}:${f.size}`).sort().join('|') || '',
    [currentTorrentFiles]
  )

  // Build cross-seed queries for all instances
  const crossSeedQueries = useMemo(() => {
    if (!allInstances || allInstances.length === 0 || !torrent || !enabled) {
      return []
    }
    
    // Wait for current files to finish loading if we're fetching them
    if (isLoadingCurrentFiles) {
      return []
    }

    const currentFiles = currentTorrentFiles || []

    return allInstances.map((instance) => ({
      queryKey: ["torrents", instance.id, "crossseed", resolvedInfohashV1, resolvedInfohashV2, torrent.name, torrent.content_path, isDiscContent, currentFilesKey],
      queryFn: () => searchCrossSeedMatches(
        torrent,
        instance,
        instanceId,
        currentFiles,
        resolvedInfohashV1,
        resolvedInfohashV2
      ),
      enabled,
      staleTime: 60000,
      gcTime: 5 * 60 * 1000,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      retry: false,
    }))
  }, [
    instanceIds,
    torrent?.hash,
    resolvedInfohashV1,
    resolvedInfohashV2,
    isDiscContent,
    enabled,
    currentFilesKey,
    allInstances,
    instanceId,
    currentTorrentFiles,
    isLoadingCurrentFiles
  ])

  // Execute cross-seed queries
  const matchingTorrentsQueries = useQueries({
    queries: crossSeedQueries,
    combine: (results) => {
      return results
    }
  })

  // Process and sort results
  const matchingTorrents = useMemo(() => {
    return matchingTorrentsQueries
      .filter((query: { isSuccess: boolean }) => query.isSuccess)
      .flatMap((query: { data?: unknown }) => (query.data as CrossSeedTorrent[]) || [])
      .sort((a, b) => {
        // Prioritize: infohash > content_path > save_path > name
        const priority = { infohash: 0, content_path: 1, save_path: 2, name: 3 }
        return priority[a.matchType] - priority[b.matchType]
      })
  }, [matchingTorrentsQueries])
  
  // Compute pending query count for granular loading indicators
  const pendingQueryCount = useMemo(() => {
    return matchingTorrentsQueries.filter((query: { isLoading: boolean }) => query.isLoading).length
  }, [matchingTorrentsQueries])
  
  const isLoadingMatches = isLoadingInstances || (matchingTorrents.length === 0 && pendingQueryCount > 0)

  return {
    matchingTorrents,
    isLoadingMatches,
    isLoadingInstances,
    pendingQueryCount,
    allInstances: allInstances || [],
  }
}