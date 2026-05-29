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
