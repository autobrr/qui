/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream, type StreamState } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { resolveStreamRow } from "@/lib/torrent-utils"
import type { SortedPeersResponse, Torrent, TorrentFile, TorrentProperties, TorrentStreamPayload, TorrentTracker, WebSeed } from "@/types"
import { focusManager, useQuery, useQueryClient, type Query } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"

export interface GeneralTabData {
  // The live row from the stream, or from the fallback poll. Null until one arrives.
  row: Torrent | null
  properties?: TorrentProperties
}

interface TabData {
  general: GeneralTabData
  content: TorrentFile[]
  pieces: number[]
  trackers: TorrentTracker[]
  peers: SortedPeersResponse
  webseeds: WebSeed[]
}

export type TorrentDetailTab = keyof TabData

export interface TorrentDetailTabResult<T extends TorrentDetailTab> {
  data: TabData[T] | undefined
  isLoading: boolean
  error: Error | null
}

interface TabQuery {
  key: string
  staleTime: number
  refetchInterval: number | false
  fetch: (instanceId: number, hash: string, refresh: boolean) => Promise<unknown>
  // reloadGap makes the tab reload on a change of progress or state in the
  // live row, not sooner than the gap after the last fetch. The tab then
  // polls at refetchInterval only while the torrent is checking or the stream
  // is not usable.
  reloadGap?: (data: unknown) => number
}

// Each query keeps the key it had in the panel, so the panel mutations that
// invalidate "torrent-peers" and "torrent-files" still reach it. React Query
// pauses each interval while the page is hidden (refetchIntervalInBackground
// is off).
const TAB_QUERIES: Record<TorrentDetailTab, TabQuery> = {
  general: { key: "torrent-properties", staleTime: 30000, refetchInterval: false, fetch: (instanceId, hash) => api.getTorrentProperties(instanceId, hash) },
  // Each torrents/files call blocks qBittorrent for a time that grows with the
  // file count, so the gap is the file count ÷ 1,000 seconds, from 5 s to 30 s.
  // refresh bypasses the 5-minute file cache of qui.
  content: {
    key: "torrent-files",
    staleTime: 3000,
    refetchInterval: 3000,
    fetch: (instanceId, hash, refresh) => api.getTorrentFiles(instanceId, hash, { refresh }),
    reloadGap: data => Math.min(30000, Math.max(5000, (data as TorrentFile[] | undefined)?.length ?? 0)),
  },
  pieces: { key: "torrent-piece-states", staleTime: 3000, refetchInterval: 5000, fetch: (instanceId, hash) => api.getTorrentPieceStates(instanceId, hash), reloadGap: () => 5000 },
  trackers: { key: "torrent-trackers", staleTime: 30000, refetchInterval: false, fetch: (instanceId, hash) => api.getTorrentTrackers(instanceId, hash) },
  peers: { key: "torrent-peers", staleTime: 0, refetchInterval: 2000, fetch: (instanceId, hash) => api.getTorrentPeers(instanceId, hash) },
  webseeds: { key: "torrent-webseeds", staleTime: 30000, refetchInterval: false, fetch: (instanceId, hash) => api.getTorrentWebSeeds(instanceId, hash) },
}

const CHECKING_STATES: ReadonlySet<string> = new Set(["checkingDL", "checkingUP", "checkingResumeData"])

// useTorrentDetailTab owns when a details tab fetches. A null hash fetches
// nothing: the panel passes null before it is ready, and for a tab that must
// not fetch while it is closed. The live row follows liveHash, which the panel
// passes before it is ready, so a switch between two torrents moves the stream
// subscription in one change.
export function useTorrentDetailTab<T extends TorrentDetailTab>(
  instanceId: number,
  hash: string | null,
  tab: T,
  liveHash: string | null = hash
): TorrentDetailTabResult<T> {
  const tabQuery = TAB_QUERIES[tab]
  const { row, streamState } = useLiveRow(instanceId, tab === "general" || tab === "trackers" || tabQuery.reloadGap ? liveHash : null, hash !== null)
  const streamUsable = isStreamUsable(streamState)
  // While a torrent is checking, Content and the piece bar poll at their old
  // interval, and Content bypasses the file cache.
  const checking = CHECKING_STATES.has(row?.state ?? "")
  const rowKey = tabQuery.reloadGap && row ? reloadKey(row) : null
  // fetched tracks the open torrent: ok is true after a fetch succeeds, and key
  // holds the row of the last fetch that bypassed the cache and succeeded, or
  // the first row that arrived. The first fetch of each torrent, and a fetch for
  // a row that changed while the stream is usable, bypass the cache. A retry or
  // a refetch from elsewhere then bypasses it too.
  const torrent = `${instanceId}|${hash}`
  const fetched = useRef({ torrent, ok: false, key: null as string | null })
  if (fetched.current.torrent !== torrent) {
    fetched.current = { torrent, ok: false, key: null }
  }
  const query = useQuery<unknown, Error>({
    queryKey: [tabQuery.key, instanceId, hash],
    queryFn: async () => {
      const key = rowKey
      const last = fetched.current
      const refresh = checking || !last.ok || (streamUsable && key !== last.key)
      const data = await tabQuery.fetch(instanceId, hash!, refresh)
      last.ok = true
      if (refresh && key !== null) {
        last.key = key
      }
      return data
    },
    enabled: hash !== null,
    staleTime: tabQuery.staleTime,
    gcTime: 5 * 60 * 1000,
    refetchInterval: !tabQuery.reloadGap ? tabQuery.refetchInterval
      : !streamUsable ? streamFallbackPollInterval(streamState, tabQuery.refetchInterval || 0)
        : checking ? tabQuery.refetchInterval : false,
    // A Trackers reload that came while the page was hidden runs when the page
    // shows again. Content and the piece bar reload only on a row change, also
    // after the network comes back.
    refetchOnWindowFocus: tab === "trackers" ? (q: Query) => q.state.isInvalidated : false,
    refetchOnReconnect: !tabQuery.reloadGap,
  })
  useTrackersReload(instanceId, tab === "trackers" ? hash : null, row)

  const visible = useSyncExternalStore(subscribeFocus, () => focusManager.isFocused())
  const gap = tabQuery.reloadGap?.(query.data) ?? 0
  const lastFetch = Math.max(query.dataUpdatedAt, query.errorUpdatedAt)
  const { isFetching, refetch } = query
  useEffect(() => {
    if (hash === null || rowKey === null) {
      return
    }
    // A fetch before the first row cannot tell a change, so it takes this row.
    const last = fetched.current
    if (last.key === null) {
      last.key = rowKey
    }
    // Until a fetch succeeds, the tab tries again after each gap.
    if (!streamUsable || checking || !visible || isFetching || (last.ok && rowKey === last.key)) {
      return
    }
    const timer = setTimeout(() => void refetch(), Math.max(0, lastFetch + gap - Date.now()))
    return () => clearTimeout(timer)
  }, [checking, gap, hash, isFetching, lastFetch, refetch, rowKey, streamUsable, visible])

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

function subscribeFocus(onChange: () => void): () => void {
  return focusManager.subscribe(onChange)
}

// reloadKey changes when the row shows a change of progress or state. A
// complete torrent goes between uploading and stalledUP while it seeds, and
// its files do not change, so for it only a check counts.
function reloadKey(row: Torrent): string {
  const state = row.progress < 1 || CHECKING_STATES.has(row.state) ? row.state : "complete"
  return `${row.progress}|${state}`
}

// mergeLiveRow puts the values of the live row over the properties that load
// one time. The other fields (comment, creator, save path) keep the loaded
// value. The piece counts come from the row only on qBittorrent 5.2 and later,
// where pieces_num is set.
function mergeLiveRow(properties: TorrentProperties, row: Torrent | null): TorrentProperties {
  if (!row) {
    return properties
  }
  return {
    ...properties,
    ...(row.pieces_num ? { pieces_have: row.pieces_have ?? 0, pieces_num: row.pieces_num } : {}),
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

// useTrackersReload reloads the Trackers tab when the tracker count or the
// Tracker health verdict of the live row changes. The qBittorrent 5.2 tracker
// flags are not in the row, so a change that is only a warning does not reload
// the tab. While the page is hidden, it only marks the list stale.
function useTrackersReload(instanceId: number, hash: string | null, row: Torrent | null) {
  const queryClient = useQueryClient()
  const torrentKey = `${instanceId}|${hash}`
  const trackerState = hash !== null && row?.hash === hash ? `${row.trackers_count}|${row.tracker_health ?? ""}` : null
  // The last state of each torrent, so a change while another torrent was open
  // still reloads the list on the return.
  const seen = useRef(new Map<string, string>())
  useEffect(() => {
    if (trackerState === null) {
      return
    }
    const last = seen.current.get(torrentKey)
    seen.current.set(torrentKey, trackerState)
    if (last === undefined || last === trackerState) {
      return
    }
    void queryClient.invalidateQueries({
      queryKey: [TAB_QUERIES.trackers.key, instanceId, hash],
      refetchType: focusManager.isFocused() ? "active" : "none",
    })
  }, [hash, instanceId, queryClient, torrentKey, trackerState])
}

// useLiveRow follows the one-row stream view of the torrent. While the stream
// is not usable and the panel is ready, it polls the row at the stream
// fallback interval.
function useLiveRow(instanceId: number, hash: string | null, ready: boolean): { row: Torrent | null; streamState: StreamState } {
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
  return { row: streamRow ?? polledRow ?? null, streamState }
}
