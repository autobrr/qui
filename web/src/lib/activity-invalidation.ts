/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { QueryClient, QueryKey } from "@tanstack/react-query"
import type { ActivityEvent, ActivityEventKind } from "@/types"

const ACTIVITY_REFRESH: Record<ActivityEventKind, (event: ActivityEvent) => QueryKey[]> = {
  "backup.run": ({ instanceId: id }) => id ? [["instance-backups", id]] : [["instance-backups"]],
  "dirscan.run": ({ resourceId }) => resourceId ? [["dir-scan", "directory", Number(resourceId)]] : [["dir-scan"]],
  "orphanscan.run": ({ instanceId: id }) => id ? [["orphan-scan", id]] : [["orphan-scan"]],
  "discscan.run": ({ instanceId: id }) => id ? [["disc-scans", id]] : [["disc-scans"]],
  "crossseed.status": () => [["cross-seed", "status"]],
  "crossseed.search": () => [["cross-seed", "search-status"], ["cross-seed", "search-runs"]],
  "reannounce.activity": ({ instanceId: id }) => id ? [["instance-reannounce-activity", id]] : [["instance-reannounce-activity"]],
  "automation.activity": ({ instanceId: id }) => id ? [["automation-activity", id]] : [["automation-activity"]],
  "indexer.activity": () => [["indexer-activity"]],
  "search.history": () => [["searchHistory"]],
  "tracker.icons": () => [["tracker-icons"]],
  "theme.settings": () => [["theme-settings"]],
  "theme.custom": () => [["custom-themes"]],
  "client.settings": () => [["client-settings"]],
}

export function activityQueryKeys(event: ActivityEvent): QueryKey[] {
  return Object.hasOwn(ACTIVITY_REFRESH, event.kind) ? ACTIVITY_REFRESH[event.kind](event) : []
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
