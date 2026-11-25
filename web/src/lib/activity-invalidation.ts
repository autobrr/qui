/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { QueryClient, QueryKey } from "@tanstack/react-query"
import type { ActivityEvent } from "@/types"

export function activityQueryKeys(event: ActivityEvent): QueryKey[] {
  const id = event.instanceId

  switch (event.kind) {
    case "backup.run":
      return id ? [["instance-backups", id]] : [["instance-backups"]]
    case "dirscan.run":
      return event.resourceId ? [["dir-scan", "directory", Number(event.resourceId)]] : [["dir-scan"]]
    case "orphanscan.run":
      return id ? [["orphan-scan", id]] : [["orphan-scan"]]
    case "discscan.run":
      return id ? [["disc-scans", id]] : [["disc-scans"]]
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
    case "theme.settings":
      return [["theme-settings"]]
    case "theme.custom":
      return [["custom-themes"]]
    case "client.settings":
      return [["client-settings"]]
    default:
      return []
  }
}

export function invalidateForActivity(queryClient: QueryClient, event: ActivityEvent): void {
  for (const queryKey of activityQueryKeys(event)) {
    queryClient.invalidateQueries({ queryKey })
  }
}

export const ACTIVITY_FEATURE_PREFIXES: QueryKey[] = [
  ["instance-backups"],
  ["dir-scan"],
  ["orphan-scan"],
  ["disc-scans"],
  ["cross-seed", "status"],
  ["cross-seed", "search-status"],
  ["cross-seed", "search-runs"],
  ["instance-reannounce-activity"],
  ["automation-activity"],
  ["indexer-activity"],
  ["searchHistory"],
  ["tracker-icons"],
  ["theme-settings"],
  ["custom-themes"],
  ["client-settings"],
]

export function invalidateAllActivity(queryClient: QueryClient): void {
  for (const queryKey of ACTIVITY_FEATURE_PREFIXES) {
    queryClient.invalidateQueries({ queryKey })
  }
}
