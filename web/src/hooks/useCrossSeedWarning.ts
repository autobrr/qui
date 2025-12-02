/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import { searchCrossSeedMatches, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { Torrent } from "@/types"

interface UseCrossSeedWarningOptions {
  instanceId: number
  instanceName: string
  torrents: Torrent[]
  enabled: boolean
}

export interface CrossSeedWarningResult {
  /** Cross-seed torrents on this instance that share files with torrents being deleted */
  affectedTorrents: CrossSeedTorrent[]
  /** Whether we're still searching for cross-seeds */
  isLoading: boolean
  /** Whether there are cross-seeds that would be affected */
  hasWarning: boolean
}

/**
 * Hook to detect cross-seeded torrents on the current instance that would be
 * affected when deleting files.
 *
 * Only checks the current instance since that's where the files live.
 * Cross-instance cross-seeds are handled by the Filter Cross-Seeds feature
 * and the Cross-Seed tab in torrent details.
 */
export function useCrossSeedWarning({
  instanceId,
  instanceName,
  torrents,
  enabled,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  // Get the first torrent to search for cross-seeds (typically deleting one at a time)
  const torrent = torrents[0]
  const hashesBeingDeleted = useMemo(
    () => new Set(torrents.map(t => t.hash)),
    [torrents]
  )

  // Fetch instance info
  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: api.getInstances,
    enabled: enabled && !!torrent,
    staleTime: 60000,
  })

  const instance = useMemo(
    () => instances?.find(i => i.id === instanceId),
    [instances, instanceId]
  )

  // Fetch torrent files for deep matching
  const { data: torrentFiles } = useQuery({
    queryKey: ["torrent-files-crossseed-warning", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentFiles(instanceId, torrent!.hash),
    enabled: enabled && !!torrent,
    staleTime: 60000,
  })

  // Search for cross-seeds using the same logic as TorrentContextMenu
  const { data: matches, isLoading } = useQuery({
    queryKey: ["cross-seed-warning", instanceId, torrent?.hash, torrent?.infohash_v1],
    queryFn: async () => {
      if (!instance || !torrent) return []
      return searchCrossSeedMatches(
        torrent,
        instance,
        instanceId,
        torrentFiles || [],
        torrent.infohash_v1 || torrent.hash,
        torrent.infohash_v2
      )
    },
    enabled: enabled && !!torrent && !!instance,
    staleTime: 10000,
    gcTime: 30000,
  })

  // Filter out torrents being deleted
  const result = useMemo((): CrossSeedWarningResult => {
    if (!enabled || !torrent) {
      return {
        affectedTorrents: [],
        isLoading: false,
        hasWarning: false,
      }
    }

    if (isLoading || !matches) {
      return {
        affectedTorrents: [],
        isLoading: true,
        hasWarning: false,
      }
    }

    // Filter out torrents that are being deleted and ensure they're on this instance
    const affectedTorrents = matches.filter(
      t => !hashesBeingDeleted.has(t.hash) && t.instanceId === instanceId
    ).map(t => ({
      ...t,
      instanceName,
    }))

    return {
      affectedTorrents,
      isLoading: false,
      hasWarning: affectedTorrents.length > 0,
    }
  }, [enabled, torrent, isLoading, matches, hashesBeingDeleted, instanceId, instanceName])

  return result
}
