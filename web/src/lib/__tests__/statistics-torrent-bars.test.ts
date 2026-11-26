/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { barBytes, buildTorrentBars, recordedUnits } from "@/lib/statistics"
import type { TransferHistoryRow } from "@/types"

function torrentRow(start: number, width: TransferHistoryRow["width"], uploaded: number): TransferHistoryRow {
  return { group: "h1", start: new Date(start).toISOString(), width, gapSeconds: 0, uploaded, downloaded: 0, statisticsCrossSeedUploaded: 0 }
}

const period = { from: new Date(Date.UTC(2025, 11, 30)), to: new Date(Date.UTC(2026, 0, 3)), unit: "day" as const }

describe("buildTorrentBars", () => {
  it("draws a folded year as one bar in place of the empty days of that year", () => {
    const bars = buildTorrentBars([torrentRow(Date.UTC(2025, 0, 1), "year", 500), torrentRow(Date.UTC(2026, 0, 2), "day", 100)], period, undefined)

    expect(bars?.map(bar => [bar.width, barBytes(bar).uploaded])).toEqual([["year", 500], ["day", 0], ["day", 100]])
  })

  it("keeps the days of a folded year from the first day that holds bytes", () => {
    const bars = buildTorrentBars([torrentRow(Date.UTC(2025, 0, 1), "year", 500), torrentRow(Date.UTC(2025, 11, 31), "day", 70)], period, undefined)

    expect(bars?.map(bar => [bar.width, barBytes(bar).uploaded])).toEqual([["year", 500], ["day", 70], ["day", 0], ["day", 0]])
  })

  it("adds up the rows of several instances in one bar for each day and each year", () => {
    const bars = buildTorrentBars([
      torrentRow(Date.UTC(2025, 0, 1), "year", 500), torrentRow(Date.UTC(2026, 0, 2), "day", 100),
      torrentRow(Date.UTC(2025, 0, 1), "year", 50), torrentRow(Date.UTC(2026, 0, 2), "day", 10),
    ], period, undefined)

    expect(bars?.map(bar => [bar.width, barBytes(bar).uploaded])).toEqual([["year", 550], ["day", 0], ["day", 110]])
  })
})

describe("recordedUnits", () => {
  it("counts the days of a year bar up to the next bar, from the first recording on", () => {
    const bars = buildTorrentBars([torrentRow(Date.UTC(2025, 0, 1), "year", 500), torrentRow(Date.UTC(2026, 0, 2), "day", 100)], period, undefined)!

    expect(recordedUnits(bars, undefined, "day", true)).toBe(365 + 2)
    expect(recordedUnits(bars, new Date(Date.UTC(2025, 11, 1, 6)), "day", true)).toBe(31 + 2)
  })

  it("ends a year bar with no bar after it at the end of the period", () => {
    const bars = buildTorrentBars([torrentRow(Date.UTC(2025, 0, 1), "year", 500)], { ...period, to: new Date(Date.UTC(2025, 11, 31, 12)) }, undefined)!

    expect(bars.map(bar => bar.width)).toEqual(["year"])
    expect(recordedUnits(bars, undefined, "day", true, new Date(Date.UTC(2025, 11, 31, 12)))).toBe(365)
    expect(recordedUnits(bars, undefined, "day", true, new Date(Date.UTC(2025, 5, 30, 12)))).toBe(181)
  })
})
