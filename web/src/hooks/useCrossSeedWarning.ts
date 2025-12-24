/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import { searchCrossSeedMatches, normalizePath, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { Torrent } from "@/types"

/**
 * Check if a path is inside a base directory.
 * Returns true if base is non-empty and path equals base or starts with base + "/".
 */
const isInsideBase = (path: string, base: string): boolean => {
  if (!base) return false
  return path === base || path.startsWith(base + "/")
}

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
      // Check each torrent for cross-seeds
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

        // Fetch files for this torrent
        let torrentFiles: Awaited<ReturnType<typeof api.getTorrentFiles>> = []
        try {
          torrentFiles = await api.getTorrentFiles(instanceId, torrent.hash)
        } catch {
          // Continue without files - will use weaker matching
        }

        // Search for cross-seeds
        const matches = await searchCrossSeedMatches(
          torrent,
          instance,
          instanceId,
          torrentFiles,
          torrent.infohash_v1 || torrent.hash,
          torrent.infohash_v2
        )

        // Filter and dedupe matches - only include matches that share the same on-disk files
        for (const match of matches) {
          // Skip torrents being deleted
          if (hashesBeingDeleted.has(match.hash)) continue
          // Skip if not on this instance
          if (match.instanceId !== instanceId) continue
          // Skip duplicates
          if (seenHashes.has(match.hash)) continue

          // Normalize match paths
          const mSave = normalizePath(match.save_path || "")
          const mContent = normalizePath(match.content_path || "")

          // Skip matches inside hardlink base directory (hardlink-mode cross-seeds)
          if (isInsideBase(mSave, hardlinkBase)) continue

          // Only include matches that share the SAME on-disk files:
          // Both save_path AND content_path must match exactly
          if (!srcSave || !srcContent || mSave !== srcSave || mContent !== srcContent) continue

          seenHashes.add(match.hash)
          allMatches.push({
            ...match,
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
