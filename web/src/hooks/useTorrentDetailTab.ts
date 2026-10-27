/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { resolveStreamRow } from "@/lib/torrent-utils"
import type { SortedPeersResponse, Torrent, TorrentProperties, TorrentStreamPayload, WebSeed } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useCallback, useMemo, useState } from "react"

export interface GeneralTabData {
  // The live row from the stream, or from the fallback poll. Null until one arrives.
  row: Torrent | null
  properties?: TorrentProperties
}

interface TabData {
  general: GeneralTabData
  peers: SortedPeersResponse
  webseeds: WebSeed[]
}

export type TorrentDetailTab = keyof TabData

export interface TorrentDetailTabResult<T extends TorrentDetailTab> {
  data: TabData[T] | undefined
  isLoading: boolean
  error: Error | null
}

// Each query keeps the key it had in the panel, so the panel mutations that
// invalidate "torrent-peers" still reach it. React Query pauses each interval
// while the page is hidden (refetchIntervalInBackground is off).
const TAB_QUERIES = {
  general: { key: "torrent-properties", staleTime: 30000, refetchInterval: false, fetch: (instanceId: number, hash: string) => api.getTorrentProperties(instanceId, hash) },
  peers: { key: "torrent-peers", staleTime: 0, refetchInterval: 2000, fetch: (instanceId: number, hash: string) => api.getTorrentPeers(instanceId, hash) },
  webseeds: { key: "torrent-webseeds", staleTime: 30000, refetchInterval: false, fetch: (instanceId: number, hash: string) => api.getTorrentWebSeeds(instanceId, hash) },
} as const

// useTorrentDetailTab owns when a details tab fetches. A null hash fetches
// nothing: the panel passes null before it is ready, and for a tab that must
// not fetch while it is closed. The live row of General follows liveHash,
// which the panel passes before it is ready, so a switch between two torrents
// moves the stream subscription in one change.
export function useTorrentDetailTab<T extends TorrentDetailTab>(
  instanceId: number,
  hash: string | null,
  tab: T,
  liveHash: string | null = hash
): TorrentDetailTabResult<T> {
  const tabQuery = TAB_QUERIES[tab]
  const row = useLiveRow(instanceId, tab === "general" ? liveHash : null, hash !== null)
  const query = useQuery<unknown, Error>({
    queryKey: [tabQuery.key, instanceId, hash],
    queryFn: () => tabQuery.fetch(instanceId, hash!),
    enabled: hash !== null,
    staleTime: tabQuery.staleTime,
    gcTime: 5 * 60 * 1000,
    refetchInterval: tabQuery.refetchInterval,
  })

  const properties = query.data as TorrentProperties | undefined
  const general = useMemo<GeneralTabData>(
    () => ({ row, properties: properties && mergeLiveRow(properties, row) }),
    [properties, row]
  )

  return {
    data: (tab === "general" ? general : query.data) as TabData[T] | undefined,
    isLoading: query.isLoading,
    error: query.error,
  }
}

// mergeLiveRow puts the values of the live row over the properties that load
// one time. The other fields (comment, creator, save path, piece counts) keep
// the loaded value.
function mergeLiveRow(properties: TorrentProperties, row: Torrent | null): TorrentProperties {
  if (!row) {
    return properties
  }
  return {
    ...properties,
    dl_speed: row.dlspeed,
    up_speed: row.upspeed,
    seeds: row.num_seeds,
    seeds_total: row.num_complete,
    peers: row.num_leechs,
    peers_total: row.num_incomplete,
    share_ratio: row.ratio,
    total_downloaded: row.downloaded,
    total_uploaded: row.uploaded,
    time_elapsed: row.time_active,
    seeding_time: row.seeding_time,
  }
}

// useLiveRow follows the one-row stream view of the torrent. While the stream
// is not usable and the panel is ready, it polls the row at the stream
// fallback interval.
function useLiveRow(instanceId: number, hash: string | null, ready: boolean): Torrent | null {
  const streamParams = useMemo(() => hash === null ? null : {
    instanceId,
    page: 0,
    limit: 1,
    sort: "added_on",
    order: "desc" as const,
    filters: {
      hashes: [hash],
      status: [],
      excludeStatus: [],
      categories: [],
      excludeCategories: [],
      tags: [],
      excludeTags: [],
      trackers: [],
      excludeTrackers: [],
    },
  }, [hash, instanceId])

  const [snapshot, setSnapshot] = useState<{ instanceId: number; hash: string; row: Torrent | null } | null>(null)
  const handleStreamPayload = useCallback((payload: TorrentStreamPayload) => {
    if (!payload?.data || hash === null) {
      return
    }
    const data = payload.data
    setSnapshot(previous => ({
      instanceId,
      hash,
      row: resolveStreamRow(previous?.instanceId === instanceId && previous.hash === hash ? previous.row : null, data),
    }))
  }, [hash, instanceId])

  const streamState = useSyncStream(streamParams, {
    enabled: streamParams !== null,
    onMessage: handleStreamPayload,
  })
  const streamUsable = isStreamUsable(streamState)
  const shouldPoll = ready && streamParams !== null && !streamUsable

  const { data: polledRow } = useQuery({
    queryKey: ["torrent-live-state", instanceId, hash],
    queryFn: async () => {
      const response = await api.getTorrents(instanceId, { filters: streamParams!.filters, limit: 1 })
      return response.torrents[0] ?? null
    },
    enabled: shouldPoll,
    staleTime: 1000,
    refetchInterval: shouldPoll ? streamFallbackPollInterval(streamState, 2000) : false,
  })

  const streamRow = streamUsable && snapshot?.instanceId === instanceId && snapshot.hash === hash ? snapshot.row : null
  return streamRow ?? polledRow ?? null
}
