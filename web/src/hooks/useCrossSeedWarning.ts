/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useCallback, useMemo, useState } from "react"

import { api } from "@/lib/api"
import { toCompatibleMatch, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { Torrent } from "@/types"

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

    try {
      // Check each torrent for cross-seeds using backend API
      for (let i = 0; i < torrents.length; i++) {
        const torrent = torrents[i]

        // Use backend API for proper release matching (rls library)
        // strict=true ensures we fail if overlap checks can't complete (delete safety)
        const matches = await api.getLocalCrossSeedMatches(instanceId, torrent.hash, true)

        // Filter and dedupe matches - only include matches that share the same content_path
        for (const match of matches) {
          // Skip if not on this instance
          if (match.instanceId !== instanceId) continue
          // Skip torrents being deleted
          if (hashesBeingDeleted.has(match.hash)) continue
          // Only include matches that share the same on-disk location
          if (match.matchType !== "content_path") continue
          // Skip duplicates
          if (seenHashes.has(match.hash)) continue

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
