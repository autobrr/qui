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
    console.log(`[CrossSeed] ========== Starting match search [ID: ${executionId}] for torrent: "${torrent.name}" on instance: ${instance.name} ==========`)
    console.log(`[CrossSeed] [${executionId}] - Hash: ${torrent.hash}`)
    console.log(`[CrossSeed] [${executionId}] - Content path: ${torrent.content_path}`)
    console.log(`[CrossSeed] [${executionId}] - Save path: ${torrent.save_path}`)
    console.log(`[CrossSeed] [${executionId}] - Size: ${torrent.size}`)
    console.log(`[CrossSeed] [${executionId}] - InfoHash v1: ${resolvedInfohashV1}`)
    console.log(`[CrossSeed] [${executionId}] - InfoHash v2: ${resolvedInfohashV2}`)
    
    // Use pre-fetched current torrent files for deep matching
    console.log(`[CrossSeed] Current torrent has ${currentFiles.length} files (for deep matching)`)
    
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
    
    console.log(`[CrossSeed] Searching with term: "${searchTerm}" (from: "${torrent.name}")`)
    const nameSearchResponse = await api.getTorrents(instance.id, {
      search: searchTerm,
      limit: 2000,
    })
    
    const nameTorrents = nameSearchResponse.torrents || []
    console.log(`[CrossSeed] Name search found ${nameTorrents.length} potential matches`)
    
    // Add name search results
    allMatches.push(...nameTorrents)
    
    // If we have info hashes, also search by them
    if (resolvedInfohashV1 || resolvedInfohashV2) {
      const hashToSearch = resolvedInfohashV1 || resolvedInfohashV2
      console.log(`[CrossSeed] Searching by info hash: ${hashToSearch}`)
      const hashSearchResponse = await api.getTorrents(instance.id, {
        search: hashToSearch,
        limit: 2000,
      })
      
      const hashTorrents = hashSearchResponse.torrents || []
      console.log(`[CrossSeed] Hash search found ${hashTorrents.length} potential matches`)
      
      // Merge results, avoiding duplicates
      let newHashMatches = 0
      for (const t of hashTorrents) {
        if (!allMatches.some(m => m.hash === t.hash)) {
          allMatches.push(t)
          newHashMatches++
        }
      }
      console.log(`[CrossSeed] Added ${newHashMatches} new matches from hash search`)
    }
    
    console.log(`[CrossSeed] Total potential matches before filtering: ${allMatches.length}`)
    
    // Normalize strings for comparison
    const normalizedContentPath = normalizePath(torrent.content_path || '')
    const normalizedName = normalizeName(torrent.name)
    
    // Filter matching torrents with different matching strategies
    console.log(`[CrossSeed] Starting match filtering with ${allMatches.length} candidates`)
    const matches = allMatches.filter((t: Torrent) => {
      console.log(`[CrossSeed] Evaluating: "${t.name}" (hash: ${t.hash})`)
      
      // Exclude the exact current torrent (same instance AND same hash)
      if (instance.id === currentInstanceId && t.hash === torrent.hash) {
        console.log(`[CrossSeed]   ❌ Skipped: Same instance and hash (current torrent)`)
        return false
      }
      
      // Strategy 1: Exact info hash match (cross-seeding same torrent)
      if ((resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) || 
          (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2)) {
        console.log(`[CrossSeed]   ✅ Match: Info hash (v1: ${t.infohash_v1 === resolvedInfohashV1}, v2: ${t.infohash_v2 === resolvedInfohashV2})`)
        return true
      }
      
      // Strategy 2: Same content path (same files, different torrent)
      if (normalizedContentPath && t.content_path) {
        const otherContentPath = normalizePath(t.content_path)
        if (otherContentPath === normalizedContentPath) {
          console.log(`[CrossSeed]   ✅ Match: Content path ("${normalizedContentPath}")`)
          return true
        }
      }
      
      // Strategy 3: Same torrent name (likely same content)
      if (normalizedName && t.name) {
        const otherName = normalizeName(t.name)
        if (otherName === normalizedName) {
          console.log(`[CrossSeed]   ✅ Match: Torrent name (exact: "${normalizedName}")`)
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
          console.log(`[CrossSeed]   Checking save path: "${normalizedCurrentSavePath}" vs "${otherSavePath}"`)
          console.log(`[CrossSeed]   Base names: "${currentBaseName}" vs "${otherBaseName}"`)
          if (currentBaseName === otherBaseName) {
            console.log(`[CrossSeed]   ✅ Match: Save path + filename`)
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
        
        console.log(`[CrossSeed]   Fuzzy match (base): "${currentNormalized}" vs "${otherNormalized}" = ${(similarity * 100).toFixed(1)}%`)
        
        // If similarity is high (>= 90%), consider it a potential match
        if (similarity >= 0.9 && currentNormalized.length > 0) {
          console.log(`[CrossSeed]   ✅ Match: Fuzzy base filename (${(similarity * 100).toFixed(1)}% similar)`)
          return true
        }
      }
      
      // Also try full name comparison with normalization
      const currentFullNormalized = normalizeFileName(torrent.name)
      const otherFullNormalized = normalizeFileName(t.name)
      const fullSimilarity = calculateSimilarity(currentFullNormalized, otherFullNormalized)
      
      console.log(`[CrossSeed]   Fuzzy match (full): "${currentFullNormalized}" vs "${otherFullNormalized}" = ${(fullSimilarity * 100).toFixed(1)}%`)
      
      if (fullSimilarity >= 0.9 && currentFullNormalized.length > 0) {
        console.log(`[CrossSeed]   ✅ Match: Fuzzy full name (${(fullSimilarity * 100).toFixed(1)}% similar)`)
        return true
      }
      
      console.log(`[CrossSeed]   ❌ No match: All strategies failed`)
      return false
    })
    
    console.log(`[CrossSeed] Filtered to ${matches.length} matches after strategy evaluation`)
    
    // For each match, check if we should do deep file matching
    console.log(`[CrossSeed] Starting deep file matching for ${matches.length} matches`)
    
    // Concurrency-limited approach to prevent spawning hundreds of simultaneous requests
    const MAX_CONCURRENT_REQUESTS = 4
    const deepMatchResults: { torrent: Torrent; isMatch: boolean; matchType: string }[] = []
    
    // Process matches in batches with concurrency control
    for (let i = 0; i < matches.length; i += MAX_CONCURRENT_REQUESTS) {
      const batch = matches.slice(i, i + MAX_CONCURRENT_REQUESTS)
      console.log(`[CrossSeed] Processing batch ${Math.floor(i / MAX_CONCURRENT_REQUESTS) + 1}/${Math.ceil(matches.length / MAX_CONCURRENT_REQUESTS)} (${batch.length} items)`)
      
      const batchResults = await Promise.all(
        batch.map(async (t: Torrent) => {
          // Skip deep matching if we already have strong matches (info hash, content path, or torrent name)
          const hasStrongMatch = 
            (resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) ||
            (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2) ||
            (normalizedContentPath && normalizePath(t.content_path) === normalizedContentPath) ||
            (normalizedName && normalizeName(t.name) === normalizedName)
          
          if (hasStrongMatch) {
            console.log(`[CrossSeed] Deep match: "${t.name}" - STRONG match, skipping deep file check`)
            return { torrent: t, isMatch: true, matchType: 'strong' }
          }
          
          // If we have files, do deep comparison
          if (currentFiles.length > 0) {
            console.log(`[CrossSeed] Deep match: "${t.name}" - Fetching files for deep comparison (current has ${currentFiles.length} files)`)
            try {
              const otherFiles = await api.getTorrentFiles(instance.id, t.hash)
              console.log(`[CrossSeed] Deep match: "${t.name}" - Other torrent has ${otherFiles.length} files`)
              
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
              
              console.log(`[CrossSeed] Deep match: "${t.name}" - File overlap: ${intersection.size}/${Math.max(currentFileSet.size, otherFileSet.size)} (${(overlapPercent * 100).toFixed(1)}%)`)
              
              if (overlapPercent > 0.8) { // 80% of files match
                console.log(`[CrossSeed] Deep match: "${t.name}" - ✅ MATCH via deep file content (${(overlapPercent * 100).toFixed(1)}% overlap)`)
                return { torrent: t, isMatch: true, matchType: 'file_content' }
              } else {
                console.log(`[CrossSeed] Deep match: "${t.name}" - ❌ Insufficient file overlap (${(overlapPercent * 100).toFixed(1)}% < 80%)`)
              }
            } catch (err) {
              console.log(`[CrossSeed] Deep match: "${t.name}" - ⚠️  Could not fetch files for deep matching:`, err)
            }
          } else {
            console.log(`[CrossSeed] Deep match: "${t.name}" - No current files available, keeping as weak match`)
          }
          
          // Keep weak matches (name, path) without deep verification
          console.log(`[CrossSeed] Deep match: "${t.name}" - Keeping as WEAK match`)
          return { torrent: t, isMatch: true, matchType: 'weak' }
        })
      )
      
      deepMatchResults.push(...batchResults)
    }
    
    console.log(`[CrossSeed] Deep matching complete, processing ${deepMatchResults.length} results`)
    
    const finalMatches = deepMatchResults.filter(r => r.isMatch).map(r => r.torrent)
    
    console.log(`[CrossSeed] Final matches after deep filtering: ${finalMatches.length}`)
    
    const enrichedMatches = finalMatches.map((t: Torrent): CrossSeedTorrent => {
      // Determine match type for display
      let matchType: CrossSeedTorrent['matchType'] = 'name'
      if ((resolvedInfohashV1 && t.infohash_v1 === resolvedInfohashV1) || 
          (resolvedInfohashV2 && t.infohash_v2 === resolvedInfohashV2)) {
        matchType = 'infohash'
        console.log(`[CrossSeed] Final: "${t.name}" -> Match type: INFO HASH`)
      } else if (normalizedContentPath && normalizePath(t.content_path) === normalizedContentPath) {
        matchType = 'content_path'
        console.log(`[CrossSeed] Final: "${t.name}" -> Match type: CONTENT PATH`)
      } else if (torrent.save_path && normalizePath(t.save_path) === normalizePath(torrent.save_path)) {
        matchType = 'save_path'
        console.log(`[CrossSeed] Final: "${t.name}" -> Match type: SAVE PATH`)
      } else {
        console.log(`[CrossSeed] Final: "${t.name}" -> Match type: NAME`)
      }
      
      return { ...t, instanceId: instance.id, instanceName: instance.name, matchType }
    })
    
    console.log(`[CrossSeed] ========== Completed search for instance ${instance.name}: ${enrichedMatches.length} matches ==========\n`)
    
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
      console.log(`[CrossSeed] ⏳ Waiting for data: instances=${!!allInstances}, instanceCount=${allInstances?.length || 0}, torrent=${!!torrent}`)
      return []
    }
    
    // Wait for current files to finish loading if we're fetching them
    if (isLoadingCurrentFiles) {
      console.log(`[CrossSeed] ⏳ Waiting for current torrent files to finish loading...`)
      return []
    }
    
    const timestamp = new Date().toISOString()
    console.log(`[CrossSeed] 🔄 REBUILDING queries for torrent "${torrent.name}" (hash: ${torrent.hash}) at ${timestamp}`)
    console.log(`[CrossSeed] - Dependency values: instanceIds="${instanceIds}", hash="${torrent.hash}", v1="${resolvedInfohashV1}", v2="${resolvedInfohashV2}", disc=${isDiscContent}, enabled=${enabled}, filesKey="${currentFilesKey}"`)
    console.log(`[CrossSeed] - Instances: ${allInstances.length} (${allInstances.map(i => i.name).join(', ')})`)
    
    const currentFiles = currentTorrentFiles || []
    
    return allInstances.map((instance) => ({
      queryKey: ["torrents", instance.id, "crossseed", resolvedInfohashV1, resolvedInfohashV2, torrent.name, torrent.content_path, isDiscContent],
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
      console.log(`[CrossSeed] useQueries combine called with ${results.length} queries, statuses:`, results.map((r, i) => `${crossSeedQueries[i]?.queryKey?.[1] || i}: ${r.status}`).join(', '))
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