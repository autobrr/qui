/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CacheMetadata, InstanceMeta, InstanceResponse, TorrentCounts, TorrentResponse } from "@/types"

export const DASHBOARD_STATS_FALLBACK_SORT = "added_on"
export const DASHBOARD_STATS_FALLBACK_ORDER = "desc"

/**
 * Query key for dashboard's lightweight stats snapshot cache.
 *
 * Stream payloads and fallback REST probes share this key so dashboard remounts
 * can paint the last cached stats immediately while the live stream reconnects.
 */
export function createDashboardStatsFallbackQueryKey(instanceId: number) {
  return [
    "dashboard-stats-fallback",
    instanceId,
    DASHBOARD_STATS_FALLBACK_SORT,
    DASHBOARD_STATS_FALLBACK_ORDER,
  ] as const
}

/**
 * Dashboard streams intentionally omit torrent counts on lightweight ticks. Treat
 * an omitted primary counts field as "no update" so hydrated tracker metadata
 * from REST/cache does not disappear as soon as the connected stream takes over.
 */
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

  if (cacheMetadata?.isStale || cacheMetadata?.source === "cache") {
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

/**
 * Once the REST fallback has returned data, dashboard status should describe
 * the active fallback path instead of a stale stream failure.
 */
export function resolveDashboardStreamError(
  streamError: string | null,
  fallbackActive: boolean,
  fallbackData: TorrentResponse | undefined
) {
  return fallbackActive && fallbackData ? null : streamError
}

/**
 * Applies live SSE instance metadata to the REST instance snapshot used by the
 * dashboard. Metadata is ignored after stream disconnect so stale stream auth or
 * connection state cannot overwrite the latest REST/fallback instance row.
 */
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

/**
 * Merges a lightweight dashboard snapshot into the previous cached snapshot.
 *
 * SSE ticks can arrive before tracker health has finished hydrating and omit
 * counts. Preserve the cached counts in that case so handoff/remount paths do
 * not regress to SSE-only data while waiting for the next hydrated tick.
 */
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
