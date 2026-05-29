/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it, vi } from "vitest"
import type { QueryClient, QueryKey } from "@tanstack/react-query"
import type { ActivityEvent } from "@/types"
import { activityQueryKeys, invalidateForActivity } from "@/lib/activity-invalidation"

function ev(partial: Partial<ActivityEvent> & Pick<ActivityEvent, "kind">): ActivityEvent {
  return { timestamp: "2026-01-01T00:00:00Z", ...partial }
}

describe("activityQueryKeys", () => {
  it("scopes instance-keyed feeds by instanceId when present", () => {
    expect(activityQueryKeys(ev({ kind: "backup.run", instanceId: 5 }))).toEqual([["instance-backups", 5]])
    expect(activityQueryKeys(ev({ kind: "orphanscan.run", instanceId: 3 }))).toEqual([["orphan-scan", 3]])
    expect(activityQueryKeys(ev({ kind: "reannounce.activity", instanceId: 9 }))).toEqual([["instance-reannounce-activity", 9]])
    expect(activityQueryKeys(ev({ kind: "automation.activity", instanceId: 2 }))).toEqual([["automation-activity", 2]])
  })

  it("falls back to the feature prefix when no instanceId is present", () => {
    expect(activityQueryKeys(ev({ kind: "backup.run" }))).toEqual([["instance-backups"]])
    expect(activityQueryKeys(ev({ kind: "orphanscan.run" }))).toEqual([["orphan-scan"]])
  })

  it("keys dir-scan by directory id carried in resourceId", () => {
    expect(activityQueryKeys(ev({ kind: "dirscan.run", resourceId: "42" }))).toEqual([["dir-scan", "directory", 42]])
    // No resourceId -> whole feature prefix.
    expect(activityQueryKeys(ev({ kind: "dirscan.run" }))).toEqual([["dir-scan"]])
  })

  it("invalidates both cross-seed search keys for a search event", () => {
    expect(activityQueryKeys(ev({ kind: "crossseed.search" }))).toEqual([
      ["cross-seed", "search-status"],
      ["cross-seed", "search-runs"],
    ])
  })

  it("uses global prefixes for global feeds", () => {
    expect(activityQueryKeys(ev({ kind: "crossseed.status" }))).toEqual([["cross-seed", "status"]])
    expect(activityQueryKeys(ev({ kind: "search.history" }))).toEqual([["searchHistory"]])
    expect(activityQueryKeys(ev({ kind: "indexer.activity" }))).toEqual([["indexer-activity"]])
    expect(activityQueryKeys(ev({ kind: "tracker.icons" }))).toEqual([["tracker-icons"]])
  })

  it("returns nothing for an unknown kind", () => {
    expect(activityQueryKeys(ev({ kind: "totally.unknown" as ActivityEvent["kind"] }))).toEqual([])
  })
})

describe("invalidateForActivity", () => {
  it("invalidates every matching key on the query client", () => {
    const invalidateQueries = vi.fn()
    const queryClient = { invalidateQueries } as unknown as QueryClient

    invalidateForActivity(queryClient, ev({ kind: "crossseed.search" }))

    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    const calledKeys = invalidateQueries.mock.calls.map(([arg]) => (arg as { queryKey: QueryKey }).queryKey)
    expect(calledKeys).toEqual([
      ["cross-seed", "search-status"],
      ["cross-seed", "search-runs"],
    ])
  })

  it("does nothing for an unknown kind", () => {
    const invalidateQueries = vi.fn()
    const queryClient = { invalidateQueries } as unknown as QueryClient
    invalidateForActivity(queryClient, ev({ kind: "nope" as ActivityEvent["kind"] }))
    expect(invalidateQueries).not.toHaveBeenCalled()
  })
})
