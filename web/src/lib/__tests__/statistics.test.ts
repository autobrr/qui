/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest"

import { bestBar, buildBars, buildCategoryBars, byteTicks, customPeriod, formatGapLength, labelledGaps, presetPeriod, previousPeriod, recordedUnits, sumBytes, topGroups } from "@/lib/statistics"
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

  it("puts a day row on its own day when the day has no midnight", () => {
    process.env.TZ = "America/Santiago"
    try {
      const row: TransferHistoryRow = { group: "a.test", start: "2026-09-06T03:00:00Z", width: "day", gapSeconds: 0, uploaded: 200, downloaded: 0, statisticsCrossSeedUploaded: 0 }
      const bars = buildBars([[row]], { from: new Date(2026, 8, 5), to: new Date(2026, 8, 7) }, "day")

      expect(bars.map(bar => [bar.start, bar.bytes.get("a.test")?.uploaded])).toEqual([[new Date(2026, 8, 5).getTime(), undefined], [new Date(2026, 8, 6).getTime(), 200]])
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

describe("buildCategoryBars", () => {
  const day = (start: string, uploaded: number): TransferHistoryRow => ({ group: "tv", start, width: "day", gapSeconds: 0, uploaded, downloaded: 0, statisticsCrossSeedUploaded: 0 })

  function inZone<T>(zone: string, run: () => T): T {
    process.env.TZ = zone
    try {
      return run()
    } finally {
      process.env.TZ = "America/New_York"
    }
  }

  // regression: #4099
  it("keeps two UTC days of a category apart when the period starts inside the second one east of UTC", () => {
    inZone("Asia/Shanghai", () => {
      // 24 hours from 03:00 on January 2 in Shanghai, which is 19:00 UTC on January 1.
      const period = { from: new Date(2026, 0, 2, 3), to: new Date(2026, 0, 3, 3), unit: "hour" as const }
      const bars = buildCategoryBars([[day("2026-01-01T00:00:00Z", 100), day("2026-01-02T00:00:00Z", 200)]], period, undefined)!

      expect(bars.map(bar => [bar.start, bar.bytes.get("tv")?.uploaded])).toEqual([[new Date(2026, 0, 1).getTime(), 100], [new Date(2026, 0, 2).getTime(), 200]])
    })
  })

  // regression: #4099
  it("counts a UTC day of a category as recorded when the first recording is late in that UTC day east of UTC", () => {
    inZone("Asia/Shanghai", () => {
      const period = { from: new Date(2026, 0, 2, 3), to: new Date(2026, 0, 3, 3), unit: "hour" as const }
      const bars = buildCategoryBars([[]], period, undefined)!

      // 22:00 UTC on January 1 is 06:00 on January 2 in Shanghai, after the end of the local January 1.
      expect(recordedUnits(bars, new Date("2026-01-01T22:00:00Z"), "day", true)).toBe(2)
    })
  })

  // regression: #4099
  it("dates each bar by its UTC day and leaves the days before the first recording out at UTC+14", () => {
    inZone("Pacific/Kiritimati", () => {
      const period = { from: new Date("2026-01-02T00:00:00Z"), to: new Date("2026-01-04T00:00:00Z"), unit: "day" as const }
      const bars = buildCategoryBars([[day("2026-01-03T00:00:00Z", 100)]], period, undefined)!

      expect(bars.map(bar => [bar.start, bar.bytes.get("tv")?.uploaded])).toEqual([[new Date(2026, 0, 2).getTime(), undefined], [new Date(2026, 0, 3).getTime(), 100]])
      expect(recordedUnits(bars, new Date("2026-01-03T12:00:00Z"), "day", true)).toBe(1)
    })
  })

  // regression: #4099
  it("leaves the days before the first recording out at UTC+13", () => {
    inZone("Pacific/Auckland", () => {
      const period = { from: new Date("2026-01-01T00:00:00Z"), to: new Date("2026-01-04T00:00:00Z"), unit: "day" as const }
      const bars = buildCategoryBars([[]], period, undefined)!

      expect(recordedUnits(bars, new Date("2026-01-02T12:00:00Z"), "day", true)).toBe(2)
    })
  })

  // regression: #4099
  it("adds no empty first day west of UTC", () => {
    const period = { from: new Date("2026-01-02T03:00:00Z"), to: new Date("2026-01-03T03:00:00Z"), unit: "hour" as const }
    const bars = buildCategoryBars([[day("2026-01-02T00:00:00Z", 100)]], period, undefined)!

    expect(bars[0].bytes.get("tv")?.uploaded).toBe(100)
  })
})

describe("bestBar", () => {
  // regression: #4099
  it("never takes a folded day of an hourly chart as the best hour", () => {
    const rows: TransferHistoryRow[] = [
      { group: "a.test", start: "2026-01-10T00:00:00Z", width: "day", gapSeconds: 0, uploaded: 500, downloaded: 0, statisticsCrossSeedUploaded: 0 },
      hourRow("2026-01-11T00:00:00Z", "a.test", 7),
    ]
    const bars = buildBars([rows], { from: new Date("2026-01-10T00:00:00Z"), to: new Date("2026-01-11T01:00:00Z") }, "hour")

    expect(bestBar(bars, "hour")?.uploaded).toBe(7)
  })
})

describe("recordedUnits", () => {
  // regression: #4099
  it("counts a folded day of an hourly chart as 24 hours", () => {
    const rows: TransferHistoryRow[] = [
      { group: "a.test", start: "2026-01-10T00:00:00Z", width: "day", gapSeconds: 0, uploaded: 500, downloaded: 0, statisticsCrossSeedUploaded: 0 },
      hourRow("2026-01-11T00:00:00Z", "a.test", 7),
    ]
    const bars = buildBars([rows], { from: new Date("2026-01-10T00:00:00Z"), to: new Date("2026-01-11T01:00:00Z") }, "hour")

    expect(recordedUnits(bars, undefined, "hour")).toBe(25)
  })

  // regression: #4099
  it("counts a 25-hour day when the first recording is in its last hour", () => {
    // In New York, November 1, 2026 has 25 hours.
    const bars = buildBars([[]], { from: new Date(2026, 10, 1), to: new Date(2026, 10, 2) }, "day")

    expect(recordedUnits(bars, new Date(2026, 10, 1, 23, 30), "day")).toBe(1)
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

  it("ends at a whole hour when the period starts inside an hour", () => {
    process.env.TZ = "Asia/Kolkata"
    try {
      const period = presetPeriod("30d", new Date("2026-01-10T14:01:00Z"))

      expect(previousPeriod(period)?.to).toEqual(new Date("2025-12-11T18:00:00Z"))
    } finally {
      process.env.TZ = "America/New_York"
    }
  })
})

describe("customPeriod", () => {
  // regression: #4084
  it("keeps hourly bars for seven dates across a change of the clock", () => {
    // In New York, October 26 to November 1, 2026 lasts 169 hours.
    expect(customPeriod(new Date(2026, 9, 26), new Date(2026, 10, 1), new Date(2026, 11, 1)).unit).toBe("hour")
    expect(customPeriod(new Date(2026, 9, 26), new Date(2026, 10, 2), new Date(2026, 11, 1)).unit).toBe("day")
  })

  // regression: #4084
  it("keeps hourly bars when the last day is in the future and the range up to now is 7 days or less", () => {
    expect(customPeriod(new Date(2026, 10, 13), new Date(2026, 11, 13), new Date(2026, 10, 13, 12)).unit).toBe("hour")
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

describe("labelledGaps", () => {
  it("labels a gap only when it is far enough from the last labelled gap", () => {
    // 365 daily bars: a label needs 61 bars of room.
    expect([...labelledGaps([300, 340, 360, 364], 365)]).toEqual([300, 364])
  })
})

describe("formatGapLength", () => {
  // regression: #4084
  it("names the units in the language of the page", () => {
    expect(formatGapLength(5400, "en")).toBe("1h 30m")
    expect(formatGapLength(5400, "zh-CN")).toBe("1小时 30分钟")
    expect(formatGapLength(30, "en")).toBe("30s")
  })
})
