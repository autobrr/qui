/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CacheMetadata, InstanceMeta, InstanceResponse, TorrentCounts, TorrentResponse, TorrentStats } from "@/types"

export const DASHBOARD_STATS_FALLBACK_SORT = "added_on"
export const DASHBOARD_STATS_FALLBACK_ORDER = "desc"

export function createDashboardStatsFallbackQueryKey(instanceId: number) {
  return [
    "dashboard-stats-fallback",
    instanceId,
    DASHBOARD_STATS_FALLBACK_SORT,
    DASHBOARD_STATS_FALLBACK_ORDER,
  ] as const
}

export function resolveDashboardTorrentCounts(
  primaryCounts: TorrentCounts | undefined,
  fallbackCounts: TorrentCounts | undefined
) {
  return primaryCounts ?? fallbackCounts
}

export function hasDashboardStatsPayload(
  data: {
    stats?: unknown
    serverState?: unknown
    counts?: unknown
    torrentCounts?: unknown
  } | null | undefined
) {
  return Boolean(data?.stats || data?.serverState || data?.counts || data?.torrentCounts)
}

export function shouldUseDashboardStatsFallback(
  streamConnected: boolean,
  hasLiveDashboardStatsPayload: boolean
) {
  return !streamConnected || !hasLiveDashboardStatsPayload
}

export type DashboardDataStatusKind = "error" | "cached" | "fallback" | "live" | null

export function resolveDashboardDataStatusKind({
  streamError,
  fallbackActive,
  cacheMetadata,
  isFirstLoad,
  streamConnected,
}: {
  streamError: string | null
  fallbackActive: boolean
  cacheMetadata: CacheMetadata | null | undefined
  isFirstLoad: boolean
  streamConnected: boolean
}): DashboardDataStatusKind {
  if (streamError) {
    return "error"
  }

  if (cacheMetadata?.source === "cache") {
    return "cached"
  }

  if (fallbackActive) {
    if (!isFirstLoad) {
      return "fallback"
    }
    return null
  }

  if (streamConnected) {
    return "live"
  }

  if (!isFirstLoad) {
    return "fallback"
  }

  return null
}

export function resolveDashboardStreamError(
  streamError: string | null,
  fallbackActive: boolean,
  fallbackData: TorrentResponse | undefined
) {
  return fallbackActive && fallbackData ? null : streamError
}

export function mergeDashboardInstanceMeta(
  instance: InstanceResponse,
  streamConnected: boolean,
  instanceMeta: InstanceMeta | null
): InstanceResponse {
  if (!streamConnected || !instanceMeta) {
    return instance
  }

  return {
    ...instance,
    connected: instanceMeta.connected,
    hasDecryptionError: instanceMeta.hasDecryptionError,
    recentErrors: instanceMeta.recentErrors,
    connectionStatus: instanceMeta.connectionStatus ?? instance.connectionStatus,
  }
}

export function mergeDashboardStatsSnapshot(
  incoming: TorrentResponse,
  cached: TorrentResponse | undefined
): TorrentResponse {
  if (!cached) {
    return incoming
  }

  return {
    ...cached,
    ...incoming,
    stats: incoming.stats ?? cached.stats,
    counts: resolveDashboardTorrentCounts(incoming.counts, cached.counts),
    serverState: incoming.serverState ?? cached.serverState,
    appInfo: incoming.appInfo ?? cached.appInfo,
    cacheMetadata: incoming.cacheMetadata ?? cached.cacheMetadata,
    instanceMeta: incoming.instanceMeta ?? cached.instanceMeta,
  }
}

export function resolveDashboardSizes(statsList: ReadonlyArray<TorrentStats | null | undefined>) {
  let totalSize = 0
  let seedingSize = 0
  let calculating = false
  for (const stats of statsList) {
    if (!stats) {
      continue
    }
    totalSize += stats.uniqueSize ?? stats.totalSize ?? 0
    seedingSize += stats.uniqueSeedingSize ?? stats.totalSeedingSize ?? 0
    calculating ||= stats.uniqueSize === undefined
  }
  return { totalSize, seedingSize, calculating }
}
