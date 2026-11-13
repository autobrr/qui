/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest"

import { buildBars, byteTicks, presetPeriod, previousPeriod, sumBytes, topGroups } from "@/lib/statistics"
import type { TransferHistoryRow } from "@/types"

const originalTZ = process.env.TZ

beforeAll(() => {
  process.env.TZ = "America/New_York"
})

afterAll(() => {
  process.env.TZ = originalTZ
})

function hourRow(start: string, group: string, uploaded: number, gapSeconds = 0): TransferHistoryRow {
  return { group, start, width: "hour", gapSeconds, uploaded, downloaded: uploaded / 2, statisticsCrossSeedUploaded: 0 }
}

describe("buildBars", () => {
  it("starts each daily bar at the midnight of the browser", () => {
    const rows = [
      hourRow("2026-01-10T03:00:00Z", "tracker.test", 100),
      hourRow("2026-01-10T05:00:00Z", "tracker.test", 200),
    ]
    const bars = buildBars([rows], { from: new Date(2026, 0, 9), to: new Date(2026, 0, 11) }, "day")

    expect(bars.map(bar => bar.start)).toEqual([new Date(2026, 0, 9).getTime(), new Date(2026, 0, 10).getTime()])
    expect(bars.map(bar => bar.bytes.get("tracker.test")?.uploaded)).toEqual([100, 200])
  })

  it("adds the instances together, so that the bars add up to the period total", () => {
    const first = [hourRow("2026-01-10T15:00:00Z", "a.test", 100), hourRow("2026-01-10T15:00:00Z", "b.test", 50)]
    const second = [hourRow("2026-01-10T15:00:00Z", "a.test", 25), hourRow("2026-01-10T16:00:00Z", "c.test", 10)]
    const bars = buildBars([first, second], { from: new Date("2026-01-10T15:00:00Z"), to: new Date("2026-01-10T17:00:00Z") }, "hour")

    const charted = bars.flatMap(bar => [...bar.bytes.values()]).reduce((sum, bytes) => sum + bytes.uploaded, 0)
    expect(charted).toBe(sumBytes([...first, ...second], false).uploaded)
    expect(bars[0].bytes.get("a.test")?.uploaded).toBe(125)
  })

  it("counts the Recording gap of a bucket one time for each instance", () => {
    const first = [hourRow("2026-01-10T15:00:00Z", "a.test", 1, 3600), hourRow("2026-01-10T15:00:00Z", "b.test", 1, 3600)]
    const second = [hourRow("2026-01-10T15:00:00Z", "a.test", 1, 600)]
    const bars = buildBars([first, second], { from: new Date("2026-01-10T15:00:00Z"), to: new Date("2026-01-10T16:00:00Z") }, "hour")

    expect(bars.map(bar => bar.gapSeconds)).toEqual([3600])
  })

  it("puts the first stored hour on the first day in a timezone with a half-hour offset", () => {
    process.env.TZ = "Asia/Kolkata"
    try {
      const bars = buildBars([[hourRow("2026-01-09T18:00:00Z", "a.test", 100)]], { from: new Date(2026, 0, 10), to: new Date(2026, 0, 11) }, "day")

      expect(bars.map(bar => [bar.start, bar.bytes.get("a.test")?.uploaded])).toEqual([[new Date(2026, 0, 10).getTime(), 100]])
    } finally {
      process.env.TZ = "America/New_York"
    }
  })

  it("draws a folded day as one day bar in an hourly chart", () => {
    const rows: TransferHistoryRow[] = [
      { group: "a.test", start: "2026-01-10T00:00:00Z", width: "day", gapSeconds: 0, uploaded: 500, downloaded: 0, statisticsCrossSeedUploaded: 0 },
      hourRow("2026-01-11T00:00:00Z", "a.test", 7),
    ]
    const bars = buildBars([rows], { from: new Date("2026-01-10T00:00:00Z"), to: new Date("2026-01-11T01:00:00Z") }, "hour")

    expect(bars.map(bar => [new Date(bar.start).toISOString(), bar.width])).toEqual([
      ["2026-01-10T00:00:00.000Z", "day"],
      ["2026-01-11T00:00:00.000Z", "hour"],
    ])
  })
})

describe("topGroups", () => {
  it("ranks the groups by the shown metric and keeps five", () => {
    const rows = ["a", "b", "c", "d", "e", "f"].map((group, i) => ({ ...hourRow("2026-01-10T00:00:00Z", group, (i + 1) * 10), downloaded: 60 - i * 10 }))

    expect(topGroups(rows, "uploaded")).toEqual(["f", "e", "d", "c", "b"])
    expect(topGroups(rows, "downloaded")).toEqual(["a", "b", "c", "d", "e"])
  })
})

describe("previousPeriod", () => {
  it("compares the whole hours of the period, without the hour that still runs", () => {
    const period = presetPeriod("24h", new Date("2026-01-10T14:01:00Z"))

    expect(previousPeriod(period)).toEqual({ from: new Date("2026-01-08T16:00:00Z"), to: new Date("2026-01-09T15:00:00Z"), unit: "hour" })
  })
})

describe("presetPeriod", () => {
  it("starts a period of days at a local midnight", () => {
    const period = presetPeriod("30d", new Date(2026, 2, 20, 15, 30))

    expect(period.from?.getTime()).toBe(new Date(2026, 1, 19).getTime())
    expect(period.unit).toBe("day")
  })
})

describe("byteTicks", () => {
  it("puts the ticks on round numbers of the byte unit", () => {
    const MiB = 1024 ** 2

    expect(byteTicks(381 * MiB)).toEqual([0, 100, 200, 300, 400].map(value => value * MiB))
  })
})
