/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: made-up dashboard data with the states that a design has to carry:
// tracker issues, a firewalled client, a busy client with a nearly full disk
// and alternative speed limits, and a client that is offline. The busy client
// has no Unique size yet, so its size carries the "calculating" mark.

import type { DashboardInstanceStats } from "@/pages/Dashboard"
import type { InstanceResponse, ServerState, TorrentCounts, TorrentStats, TrackerTransferStats } from "@/types"
import { useEffect, useState } from "react"

const KIB = 1024
const MIB = KIB * 1024
const GIB = MIB * 1024
const TIB = GIB * 1024

const TRACKERS = ["aurora.example", "bitlantis.example", "cinder.example", "driftwood.example", "ember.example", "fjord.example", "glacier.example", "harbor.example", "isotope.example", "juniper.example", "kelp.example", "lantern.example"]

function trackerTransfers(scale: number, take: number): Record<string, TrackerTransferStats> {
  return Object.fromEntries(TRACKERS.slice(0, take).map((domain, i) => {
    const weight = scale / (i + 1.4)
    return [domain, {
      uploaded: weight * 62 * TIB,
      downloaded: weight * (i % 4 === 3 ? 31 : 9) * TIB,
      uploadedSession: weight * 400 * GIB,
      downloadedSession: weight * 60 * GIB,
      totalSize: weight * 5 * TIB,
      count: Math.round(weight * 1500),
    }]
  }))
}

function instance(id: number, name: string, host: string, extra: Partial<InstanceResponse> = {}): InstanceResponse {
  return {
    id, name, host, username: "admin", isActive: true, sortOrder: id, connected: true, hasDecryptionError: false,
    connectionStatus: "connected", reannounceSettings: { enabled: id === 1 },
    ...extra,
  } as InstanceResponse
}

function counts(total: number, status: Record<string, number>, transfers: Record<string, TrackerTransferStats>): TorrentCounts {
  return { status, categories: {}, tags: {}, trackers: {}, trackerTransfers: transfers, total }
}

function serverState(s: Partial<ServerState>): ServerState {
  return {
    connection_status: "connected", dht_nodes: 384, dl_info_data: 0, dl_info_speed: 0, dl_rate_limit: 0, up_info_data: 0,
    up_info_speed: 0, up_rate_limit: 0, queueing: true, use_alt_speed_limits: false, refresh_interval: 1500,
    queued_io_jobs: 0, total_buffers_size: 12 * MIB, total_queued_size: 0, average_time_queue: 3,
    ...s,
  }
}

const launch = (days: number) => Math.floor(Date.now() / 1000) - days * 86400 - 4 * 3600
const appInfo = (days: number) => ({ version: "v5.2.4", webAPIVersion: "2.15.1", buildInfo: { libtorrent: "1.2.20.0" }, processInfo: { launchTime: launch(days) } }) as DashboardInstanceStats["appInfo"]

function row(d: Pick<DashboardInstanceStats, "instance" | "stats" | "serverState" | "torrentCounts" | "appInfo">): DashboardInstanceStats {
  return {
    ...d,
    altSpeedEnabled: d.serverState?.use_alt_speed_limits ?? false,
    isLoading: false, error: null, streamConnected: true, streamError: null, hasLiveDashboardStatsPayload: true,
    isDashboardStatsFallbackActive: false, cacheMetadata: null, instanceMeta: null,
  }
}

const stats = (s: Partial<TorrentStats>): TorrentStats => ({ total: 0, downloading: 0, seeding: 0, paused: 0, error: 0, ...s })

const BASE: DashboardInstanceStats[] = [
  row({
    instance: instance(1, "main", "http://10.0.0.60:8080"),
    stats: stats({ total: 6152, seeding: 5890, paused: 258, totalDownloadSpeed: 0, totalUploadSpeed: 746 * KIB, totalSize: 27.3 * TIB, totalSeedingSize: 26.9 * TIB, uniqueSize: 20.41 * TIB, uniqueSeedingSize: 20.1 * TIB }),
    serverState: serverState({ free_space_on_disk: 16.46 * TIB, alltime_ul: 412 * TIB, alltime_dl: 98 * TIB, up_info_data: 38 * GIB, dl_info_data: 2 * GIB, total_peer_connections: 212, last_external_address_v4: "203.0.113.24" }),
    torrentCounts: counts(6152, { downloading: 0, active: 4, seeding: 5890, stopped: 258, unregistered: 1, tracker_down: 247, tracker_error: 1, errored: 0 }, trackerTransfers(1, 12)),
    appInfo: appInfo(12),
  }),
  row({
    instance: instance(2, "secondary", "http://10.0.0.60:18081", { connectionStatus: "firewalled" }),
    stats: stats({ total: 760, seeding: 741, paused: 19, totalDownloadSpeed: 0, totalUploadSpeed: 126 * KIB, totalSize: 4.02 * TIB, totalSeedingSize: 3.9 * TIB, uniqueSize: 3.18 * TIB, uniqueSeedingSize: 3.1 * TIB }),
    serverState: serverState({ connection_status: "firewalled", free_space_on_disk: 16.46 * TIB, alltime_ul: 22 * TIB, alltime_dl: 14 * TIB, up_info_data: 4 * GIB, total_peer_connections: 31 }),
    torrentCounts: counts(760, { downloading: 0, active: 1, seeding: 741, stopped: 19 }, trackerTransfers(0.12, 5)),
    appInfo: appInfo(12),
  }),
  row({
    instance: instance(3, "seedbox", "https://box.example:443"),
    stats: stats({ total: 124, downloading: 3, seeding: 118, paused: 3, totalDownloadSpeed: 12.4 * MIB, totalUploadSpeed: 2.1 * MIB, totalSize: 1.12 * TIB, totalSeedingSize: 1.05 * TIB, totalRemainingSize: 41 * GIB }),
    serverState: serverState({ use_alt_speed_limits: true, free_space_on_disk: 38 * GIB, alltime_ul: 9.4 * TIB, alltime_dl: 1.3 * TIB, up_info_data: 61 * GIB, dl_info_data: 88 * GIB, total_peer_connections: 487, queued_io_jobs: 14, last_external_address_v4: "198.51.100.7" }),
    torrentCounts: counts(124, { downloading: 3, active: 9, seeding: 118, stopped: 3, errored: 2 }, trackerTransfers(0.05, 3)),
    appInfo: appInfo(2),
  }),
  row({
    instance: instance(4, "backup", "http://10.0.0.71:8080", {
      connected: false, connectionStatus: "disconnected",
      recentErrors: [{ id: 1, instanceId: 4, errorType: "connection", errorMessage: "connection refused", occurredAt: new Date().toISOString() }],
    }),
    stats: null, serverState: null, torrentCounts: undefined, appInfo: null,
  }),
]

const NONE: DashboardInstanceStats[] = []

/** The fixture rows, with speeds that drift every two seconds so that live parts move. */
export function useFixtureStats(enabled: boolean): DashboardInstanceStats[] {
  const [rows, setRows] = useState(BASE)

  useEffect(() => {
    if (!enabled) return
    const id = setInterval(() => {
      setRows(prev => prev.map((d, i) => {
        const base = BASE[i].stats
        if (!d.stats || !base) return d
        const drift = (now: number | undefined, target: number | undefined) => {
          if (!target) return 0
          const next = (now || target) * (0.86 + Math.random() * 0.28)
          return Math.min(target * 1.6, Math.max(target * 0.4, next))
        }
        return { ...d, stats: { ...d.stats, totalDownloadSpeed: drift(d.stats.totalDownloadSpeed, base.totalDownloadSpeed), totalUploadSpeed: drift(d.stats.totalUploadSpeed, base.totalUploadSpeed) } }
      }))
    }, 2000)
    return () => clearInterval(id)
  }, [enabled])

  return enabled ? rows : NONE
}
