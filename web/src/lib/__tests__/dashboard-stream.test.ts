/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import {
  DASHBOARD_STATS_FALLBACK_ORDER,
  DASHBOARD_STATS_FALLBACK_SORT,
  createDashboardStatsFallbackQueryKey,
  mergeDashboardStatsSnapshot,
  resolveDashboardTorrentCounts
} from "@/lib/dashboard-stream"
import type { TorrentCounts } from "@/types"

function makeCounts(overrides: Partial<TorrentCounts> = {}): TorrentCounts {
  return {
    status: { all: 1 },
    categories: {},
    tags: {},
    trackers: { "tracker.example": 1 },
    trackerTransfers: {
      "tracker.example": {
        uploaded: 10,
        downloaded: 20,
        totalSize: 30,
        count: 1,
      },
    },
    total: 1,
    ...overrides,
  }
}

describe("resolveDashboardTorrentCounts", () => {
  it("preserves hydrated counts when a lightweight stream tick omits counts", () => {
    const currentCounts = makeCounts()

    expect(resolveDashboardTorrentCounts(undefined, currentCounts)).toBe(currentCounts)
  })

  it("uses incoming counts when the stream includes them", () => {
    const currentCounts = makeCounts()
    const incomingCounts = makeCounts({ status: { all: 0 }, trackers: {}, trackerTransfers: {}, total: 0 })

    expect(resolveDashboardTorrentCounts(incomingCounts, currentCounts)).toBe(incomingCounts)
  })
})

describe("createDashboardStatsFallbackQueryKey", () => {
  it("uses the same lightweight scope for stream snapshots and fallback probes", () => {
    expect(createDashboardStatsFallbackQueryKey(7)).toEqual([
      "dashboard-stats-fallback",
      7,
      DASHBOARD_STATS_FALLBACK_SORT,
      DASHBOARD_STATS_FALLBACK_ORDER,
    ])
  })
})

describe("mergeDashboardStatsSnapshot", () => {
  it("preserves cached tracker counts when an incoming lightweight snapshot omits counts", () => {
    const cachedCounts = makeCounts()
    const merged = mergeDashboardStatsSnapshot(
      {
        torrents: [],
        total: 1,
        stats: {
          totalDownloadSpeed: 1,
          totalUploadSpeed: 2,
          totalSize: 3,
          totalRemainingSize: 4,
          totalSeedingSize: 5,
          downloading: 6,
          seeding: 7,
          total: 8,
          paused: 9,
          error: 10,
        },
      },
      {
        torrents: [],
        total: 1,
        counts: cachedCounts,
      }
    )

    expect(merged.counts).toBe(cachedCounts)
    expect(merged.stats?.totalDownloadSpeed).toBe(1)
  })
})
