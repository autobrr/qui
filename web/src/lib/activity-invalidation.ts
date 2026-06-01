/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { QueryClient, QueryKey } from "@tanstack/react-query"
import type { ActivityEvent } from "@/types"

/**
 * activityQueryKeys returns the react-query key prefixes to invalidate for a
 * qui-owned server activity event.
 *
 * invalidateQueries matches by prefix, so a returned key like
 * ["dir-scan", "directory", 5] also invalidates
 * ["dir-scan", "directory", 5, "runs", 10] etc. Events that carry an instanceId
 * scope invalidation to that instance; without one we fall back to the whole
 * feature prefix.
 */
export function activityQueryKeys(event: ActivityEvent): QueryKey[] {
  const id = event.instanceId

  switch (event.kind) {
    case "backup.run":
      return id ? [["instance-backups", id]] : [["instance-backups"]]
    case "dirscan.run":
      return event.resourceId ? [["dir-scan", "directory", Number(event.resourceId)]] : [["dir-scan"]]
    case "orphanscan.run":
      return id ? [["orphan-scan", id]] : [["orphan-scan"]]
    case "crossseed.status":
      return [["cross-seed", "status"]]
    case "crossseed.search":
      return [["cross-seed", "search-status"], ["cross-seed", "search-runs"]]
    case "reannounce.activity":
      return id ? [["instance-reannounce-activity", id]] : [["instance-reannounce-activity"]]
    case "automation.activity":
      return id ? [["automation-activity", id]] : [["automation-activity"]]
    case "indexer.activity":
      return [["indexer-activity"]]
    case "search.history":
      return [["searchHistory"]]
    case "tracker.icons":
      return [["tracker-icons"]]
    default:
      return []
  }
}

/** invalidateForActivity invalidates every query key matching the event. */
export function invalidateForActivity(queryClient: QueryClient, event: ActivityEvent): void {
  for (const queryKey of activityQueryKeys(event)) {
    queryClient.invalidateQueries({ queryKey })
  }
}

/**
 * ACTIVITY_FEATURE_PREFIXES is the union of every activity-backed query family,
 * at feature-prefix granularity (no instance/resource scoping). It must cover the
 * root of every key activityQueryKeys can emit; the drift-guard test enforces that.
 *
 * It is used on stream reconnect to reconcile feeds that dropped their idle
 * refetch interval: activity events emitted while the EventSource was down are not
 * replayed to the fresh per-session topic, so without this a missed event would
 * leave a feed stale until the next event.
 */
export const ACTIVITY_FEATURE_PREFIXES: QueryKey[] = [
  ["instance-backups"],
  ["dir-scan"],
  ["orphan-scan"],
  ["cross-seed", "status"],
  ["cross-seed", "search-status"],
  ["cross-seed", "search-runs"],
  ["instance-reannounce-activity"],
  ["automation-activity"],
  ["indexer-activity"],
  ["searchHistory"],
  ["tracker-icons"],
]

/**
 * invalidateAllActivity invalidates every activity-backed query family. Prefix
 * matching means only mounted (active) queries refetch; inactive ones are just
 * marked stale. Call this when the activity stream reconnects so idle feeds
 * reconcile after a gap, without reintroducing timer-based polling.
 */
export function invalidateAllActivity(queryClient: QueryClient): void {
  for (const queryKey of ACTIVITY_FEATURE_PREFIXES) {
    queryClient.invalidateQueries({ queryKey })
  }
}
