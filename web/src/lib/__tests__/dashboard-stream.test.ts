/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { resolveDashboardTorrentCounts } from "@/lib/dashboard-stream"
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
