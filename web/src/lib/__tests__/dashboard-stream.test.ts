/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import {
  DASHBOARD_STATS_FALLBACK_ORDER,
  DASHBOARD_STATS_FALLBACK_SORT,
  createDashboardStatsFallbackQueryKey,
  mergeDashboardInstanceMeta,
  mergeDashboardStatsSnapshot,
  resolveDashboardStreamError,
  resolveDashboardTorrentCounts
} from "@/lib/dashboard-stream"
import type { InstanceResponse, TorrentCounts } from "@/types"

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

function makeInstance(overrides: Partial<InstanceResponse> = {}): InstanceResponse {
  return {
    id: 7,
    name: "qbit",
    host: "http://localhost:8080",
    username: "user",
    tlsSkipVerify: false,
    hasLocalFilesystemAccess: true,
    useHardlinks: false,
    hardlinkBaseDir: "",
    hardlinkDirPreset: "flat",
    useReflinks: false,
    fallbackToRegularMode: true,
    sortOrder: 0,
    isActive: true,
    reannounceSettings: {
      enabled: false,
      initialWaitSeconds: 5,
      reannounceIntervalSeconds: 60,
      maxAgeSeconds: 3600,
      maxRetries: 3,
      aggressive: false,
      monitorAll: false,
      excludeCategories: false,
      categories: [],
      excludeTags: false,
      tags: [],
      excludeTrackers: false,
      trackers: [],
    },
    connected: false,
    hasDecryptionError: true,
    recentErrors: [{
      id: 1,
      instanceId: 7,
      errorType: "auth",
      errorMessage: "old",
      occurredAt: "2026-06-29T00:00:00Z",
    }],
    connectionStatus: "disconnected",
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

describe("resolveDashboardStreamError", () => {
  it("clears stale stream errors after fallback data succeeds", () => {
    expect(resolveDashboardStreamError("stream down", true, { torrents: [], total: 0 })).toBeNull()
  })

  it("keeps stream errors until fallback has usable data", () => {
    expect(resolveDashboardStreamError("stream down", true, undefined)).toBe("stream down")
  })

  it("keeps stream errors while the stream is active", () => {
    expect(resolveDashboardStreamError("stream down", false, { torrents: [], total: 0 })).toBe("stream down")
  })
})

describe("mergeDashboardInstanceMeta", () => {
  it("uses live SSE auth and connection fields when stream metadata is connected", () => {
    const merged = mergeDashboardInstanceMeta(makeInstance(), true, {
      connected: true,
      hasDecryptionError: false,
      recentErrors: [],
      connectionStatus: "connected",
    })

    expect(merged.connected).toBe(true)
    expect(merged.hasDecryptionError).toBe(false)
    expect(merged.recentErrors).toEqual([])
    expect(merged.connectionStatus).toBe("connected")
  })

  it("ignores stale SSE metadata after the stream disconnects", () => {
    const instance = makeInstance({ connected: true, hasDecryptionError: false, connectionStatus: "connected" })

    expect(mergeDashboardInstanceMeta(instance, false, {
      connected: false,
      hasDecryptionError: true,
      connectionStatus: "disabled",
    })).toBe(instance)
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
