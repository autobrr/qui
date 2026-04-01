/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useCallback, useMemo, useState } from "react"

import { api } from "@/lib/api"
import { toCompatibleMatch, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { isAllInstancesScope } from "@/lib/instances"
import type { CrossInstanceTorrent, Torrent } from "@/types"

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

function isCrossInstanceTorrent(t: Torrent): t is CrossInstanceTorrent {
  return "instanceId" in t && typeof (t as CrossInstanceTorrent).instanceId === "number"
}

/**
 * Hook to detect cross-seeded torrents that would be affected when deleting files.
 *
 * Search is opt-in - call `search()` to check for cross-seeds.
 * Checks ALL selected torrents, not just the first one.
 *
 * In unified (all-instances) view, resolves each torrent's instance from its
 * own instanceId field (CrossInstanceTorrent) and checks per-instance.
 */
export function useCrossSeedWarning({
  instanceId,
  instanceName,
  torrents,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  const [searchState, setSearchState] = useState<CrossSeedSearchState>("idle")
  const [affectedTorrents, setAffectedTorrents] = useState<CrossSeedTorrent[]>([])
  const [checkedCount, setCheckedCount] = useState(0)

  const isUnified = isAllInstancesScope(instanceId)

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
    if (torrents.length === 0) return

    // In single-instance view, we need the instance to exist.
    // In unified view, we resolve per torrent.
    if (!isUnified && !instance) return

    setSearchState("searching")
    setCheckedCount(0)
    setAffectedTorrents([])

    const allMatches: CrossSeedTorrent[] = []
    const seenKeys = new Set<string>()

    // Build a lookup for instance names when in unified view
    const instanceNameLookup = new Map<number, string>()
    if (isUnified && instances) {
      for (const inst of instances) {
        instanceNameLookup.set(inst.id, inst.name)
      }
    }

    try {
      for (let i = 0; i < torrents.length; i++) {
        const torrent = torrents[i]

        // Resolve the instance ID and name for this specific torrent
        let torrentInstanceId: number
        let torrentInstanceName: string

        if (isUnified && isCrossInstanceTorrent(torrent)) {
          torrentInstanceId = torrent.instanceId
          torrentInstanceName = instanceNameLookup.get(torrent.instanceId)
            ?? torrent.instanceName
            ?? `Instance ${torrent.instanceId}`
        } else {
          torrentInstanceId = instanceId
          torrentInstanceName = instanceName
        }

        const matches = await api.getLocalCrossSeedMatches(torrentInstanceId, torrent.hash, true)

        for (const match of matches) {
          // Skip if not on the same instance as the torrent being deleted
          if (match.instanceId !== torrentInstanceId) continue
          // Skip torrents being deleted
          if (hashesBeingDeleted.has(match.hash)) continue
          // Only include matches that share the same on-disk bytes
          if (match.matchType !== "content_path") continue
          // Skip duplicates (instance-aware to handle same hash on multiple instances)
          const dedupeKey = isUnified ? `${match.instanceId}:${match.hash}` : match.hash
          if (seenKeys.has(dedupeKey)) continue

          seenKeys.add(dedupeKey)
          allMatches.push({
            ...toCompatibleMatch(match),
            instanceName: torrentInstanceName,
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
  }, [instance, instances, isUnified, torrents, instanceId, instanceName, hashesBeingDeleted])

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
