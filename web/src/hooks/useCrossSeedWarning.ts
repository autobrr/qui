/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useCallback, useMemo, useState } from "react"

import { api } from "@/lib/api"
import { isInsideBase, normalizePath, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { LocalCrossSeedMatch, Torrent } from "@/types"

interface UseCrossSeedWarningOptions {
  instanceId: number
  instanceName: string
  torrents: Torrent[]
}

export type CrossSeedSearchState = "idle" | "searching" | "complete" | "error"

export interface CrossSeedWarningResult {
  /** Cross-seed torrents on this instance that share files with torrents being deleted */
  affectedTorrents: CrossSeedTorrent[]
  /** Current search state */
  searchState: CrossSeedSearchState
  /** Whether there are cross-seeds that would be affected */
  hasWarning: boolean
  /** Number of torrents being checked */
  totalToCheck: number
  /** Number of torrents checked so far */
  checkedCount: number
  /** Trigger the cross-seed search */
  search: () => void
  /** Reset the search state */
  reset: () => void
}

/**
 * Convert LocalCrossSeedMatch to CrossSeedTorrent for backward compatibility.
 */
function toCompatibleMatch(m: LocalCrossSeedMatch): CrossSeedTorrent {
  return {
    hash: m.hash,
    name: m.name,
    size: m.size,
    progress: m.progress,
    save_path: m.savePath,
    content_path: m.contentPath,
    category: m.category,
    tags: m.tags,
    state: m.state,
    tracker: m.tracker,
    tracker_health: m.trackerHealth as "unregistered" | "tracker_down" | undefined,
    instanceId: m.instanceId,
    instanceName: m.instanceName,
    matchType: m.matchType,
    // Default values for required Torrent fields
    added_on: 0,
    completion_on: 0,
    dlspeed: 0,
    downloaded: 0,
    downloaded_session: 0,
    eta: 0,
    num_leechs: 0,
    num_seeds: 0,
    priority: 0,
    seq_dl: false,
    f_l_piece_prio: false,
    super_seeding: false,
    force_start: false,
    auto_tmm: false,
    seen_complete: 0,
    time_active: 0,
    num_complete: 0,
    num_incomplete: 0,
    amount_left: 0,
    completed: 0,
    last_activity: 0,
    magnet_uri: "",
    availability: 0,
    dl_limit: 0,
    download_path: "",
    infohash_v1: "",
    infohash_v2: "",
    popularity: 0,
    private: false,
    max_ratio: 0,
    max_seeding_time: 0,
    seeding_time: 0,
    ratio: 0,
    ratio_limit: 0,
    reannounce: 0,
    seeding_time_limit: 0,
    total_size: m.size,
    trackers_count: 0,
    up_limit: 0,
    uploaded: 0,
    uploaded_session: 0,
    upspeed: 0,
  }
}

/**
 * Hook to detect cross-seeded torrents on the current instance that would be
 * affected when deleting files.
 *
 * Search is opt-in - call `search()` to check for cross-seeds.
 * Checks ALL selected torrents, not just the first one.
 */
export function useCrossSeedWarning({
  instanceId,
  instanceName,
  torrents,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  const [searchState, setSearchState] = useState<CrossSeedSearchState>("idle")
  const [affectedTorrents, setAffectedTorrents] = useState<CrossSeedTorrent[]>([])
  const [checkedCount, setCheckedCount] = useState(0)

  const hashesBeingDeleted = useMemo(
    () => new Set(torrents.map(t => t.hash)),
    [torrents]
  )

  // Fetch instance info (always enabled so it's ready when user clicks search)
  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: api.getInstances,
    staleTime: 60000,
  })

  const instance = useMemo(
    () => instances?.find(i => i.id === instanceId),
    [instances, instanceId]
  )

  const search = useCallback(async () => {
    if (!instance || torrents.length === 0) return

    setSearchState("searching")
    setCheckedCount(0)
    setAffectedTorrents([])

    const allMatches: CrossSeedTorrent[] = []
    const seenHashes = new Set<string>()

    // Get hardlink base directory for this instance (to exclude hardlink-mode torrents)
    const hardlinkBase = normalizePath(instance.hardlinkBaseDir || "")

    try {
      // Check each torrent for cross-seeds using backend API
      for (let i = 0; i < torrents.length; i++) {
        const torrent = torrents[i]

        // Normalize source torrent paths
        const srcSave = normalizePath(torrent.save_path || "")
        const srcContent = normalizePath(torrent.content_path || "")

        // Skip source torrents inside hardlink base (they don't share files with originals)
        if (isInsideBase(srcSave, hardlinkBase)) {
          setCheckedCount(i + 1)
          continue
        }

        // Use backend API for proper release matching (rls library)
        const matches = await api.getLocalCrossSeedMatches(instanceId, torrent.hash)

        // Filter and dedupe matches - only include matches that share the same on-disk files
        for (const match of matches) {
          // Skip torrents being deleted
          if (hashesBeingDeleted.has(match.hash)) continue
          // Skip if not on this instance
          if (match.instanceId !== instanceId) continue
          // Skip duplicates
          if (seenHashes.has(match.hash)) continue

          // Normalize match paths
          const mSave = normalizePath(match.savePath || "")
          const mContent = normalizePath(match.contentPath || "")

          // Skip matches inside hardlink base directory (hardlink-mode cross-seeds)
          if (isInsideBase(mSave, hardlinkBase)) continue

          // Only include matches that share the SAME on-disk files:
          // Both save_path AND content_path must match exactly
          if (!srcSave || !srcContent || mSave !== srcSave || mContent !== srcContent) continue

          seenHashes.add(match.hash)
          allMatches.push({
            ...toCompatibleMatch(match),
            instanceName,
          })
        }

        setCheckedCount(i + 1)
      }

      setAffectedTorrents(allMatches)
      setSearchState("complete")
    } catch (error) {
      console.error("[CrossSeedWarning] Search failed:", error)
      setSearchState("error")
    }
  }, [instance, torrents, instanceId, instanceName, hashesBeingDeleted])

  const reset = useCallback(() => {
    setSearchState("idle")
    setAffectedTorrents([])
    setCheckedCount(0)
  }, [])

  return {
    affectedTorrents,
    searchState,
    hasWarning: affectedTorrents.length > 0,
    totalToCheck: torrents.length,
    checkedCount,
    search,
    reset,
  }
}
