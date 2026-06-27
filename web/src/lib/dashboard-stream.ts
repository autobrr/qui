/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentCounts, TorrentResponse } from "@/types"

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
