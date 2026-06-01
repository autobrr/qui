/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it, vi } from "vitest"
import type { QueryClient, QueryKey } from "@tanstack/react-query"
import type { ActivityEvent } from "@/types"
import {
  ACTIVITY_FEATURE_PREFIXES,
  activityQueryKeys,
  invalidateAllActivity,
  invalidateForActivity
} from "@/lib/activity-invalidation"

// Every activity kind the backend can emit. The `satisfies` check is compiler-
// enforced exhaustiveness: adding a kind to the ActivityEvent union without listing
// it here fails to compile, so the drift-guard test below can never silently miss a
// kind (and thus a feed family left out of ACTIVITY_FEATURE_PREFIXES).
const ALL_KINDS_MAP = {
  "backup.run": true,
  "dirscan.run": true,
  "orphanscan.run": true,
  "crossseed.status": true,
  "crossseed.search": true,
  "reannounce.activity": true,
  "automation.activity": true,
  "indexer.activity": true,
  "search.history": true,
  "tracker.icons": true,
} satisfies Record<ActivityEvent["kind"], true>

const ALL_KINDS = Object.keys(ALL_KINDS_MAP) as ActivityEvent["kind"][]

function isPrefixOf(prefix: QueryKey, key: QueryKey): boolean {
  return prefix.length <= key.length && prefix.every((segment, i) => segment === key[i])
}

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

describe("invalidateAllActivity", () => {
  it("invalidates every activity feature prefix exactly once", () => {
    const invalidateQueries = vi.fn()
    const queryClient = { invalidateQueries } as unknown as QueryClient

    invalidateAllActivity(queryClient)

    expect(invalidateQueries).toHaveBeenCalledTimes(ACTIVITY_FEATURE_PREFIXES.length)
    const calledKeys = invalidateQueries.mock.calls.map(([arg]) => (arg as { queryKey: QueryKey }).queryKey)
    expect(calledKeys).toEqual(ACTIVITY_FEATURE_PREFIXES)
  })

  // Drift guard: reconnect reconciliation must cover every kind. If a new kind maps
  // to a key family not represented in ACTIVITY_FEATURE_PREFIXES, a missed event on
  // that feed would never reconcile on reconnect.
  it("covers the key family of every activity kind", () => {
    for (const kind of ALL_KINDS) {
      const keys = activityQueryKeys(ev({ kind }))
      expect(keys.length).toBeGreaterThan(0)
      for (const key of keys) {
        const covered = ACTIVITY_FEATURE_PREFIXES.some(prefix => isPrefixOf(prefix, key))
        expect(covered, `no feature prefix covers ${JSON.stringify(key)} (kind ${kind})`).toBe(true)
      }
    }
  })
})
