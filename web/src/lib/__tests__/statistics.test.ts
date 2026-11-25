/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest"

import { bestBar, buildBars, buildCategoryBars, byteTicks, customPeriod, formatGapLength, hourAxis, labelledGaps, presetPeriod, previousPeriod, recordedUnits, sumBytes, topGroups, type StatisticsBar } from "@/lib/statistics"
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

  it("keeps a folded UTC day on its own date at UTC+13", () => {
    process.env.TZ = "Pacific/Auckland"
    try {
      const rows: TransferHistoryRow[] = [
        { group: "a.test", start: "2025-01-31T00:00:00Z", width: "day", gapSeconds: 0, uploaded: 100, downloaded: 0, statisticsCrossSeedUploaded: 0 },
        { group: "a.test", start: "2025-01-31T11:00:00Z", width: "day", gapSeconds: 0, uploaded: 7, downloaded: 0, statisticsCrossSeedUploaded: 0 },
      ]
      const bars = buildBars([rows], { from: new Date(2025, 0, 30), to: new Date(2025, 1, 2) }, "day")

      expect(bars.map(bar => [new Date(bar.start).toDateString(), bar.bytes.get("a.test")?.uploaded])).toEqual([
        ["Thu Jan 30 2025", undefined],
        ["Fri Jan 31 2025", 100],
        ["Sat Feb 01 2025", 7],
      ])
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

  it("keeps two UTC days of a category apart when the period starts inside the second one east of UTC", () => {
    inZone("Asia/Shanghai", () => {
      const period = { from: new Date(2026, 0, 2, 3), to: new Date(2026, 0, 3, 3), unit: "hour" as const }
      const bars = buildCategoryBars([[day("2026-01-01T00:00:00Z", 100), day("2026-01-02T00:00:00Z", 200)]], period, undefined)!

      expect(bars.map(bar => [bar.start, bar.bytes.get("tv")?.uploaded])).toEqual([[new Date(2026, 0, 1).getTime(), 100], [new Date(2026, 0, 2).getTime(), 200]])
    })
  })

  it("counts a UTC day of a category as recorded when the first recording is late in that UTC day east of UTC", () => {
    inZone("Asia/Shanghai", () => {
      const period = { from: new Date(2026, 0, 2, 3), to: new Date(2026, 0, 3, 3), unit: "hour" as const }
      const bars = buildCategoryBars([[]], period, undefined)!

      expect(recordedUnits(bars, new Date("2026-01-01T22:00:00Z"), "day", true)).toBe(2)
    })
  })

  it("dates each bar by its UTC day and leaves the days before the first recording out at UTC+14", () => {
    inZone("Pacific/Kiritimati", () => {
      const period = { from: new Date("2026-01-02T00:00:00Z"), to: new Date("2026-01-04T00:00:00Z"), unit: "day" as const }
      const bars = buildCategoryBars([[day("2026-01-03T00:00:00Z", 100)]], period, undefined)!

      expect(bars.map(bar => [bar.start, bar.bytes.get("tv")?.uploaded])).toEqual([[new Date(2026, 0, 2).getTime(), undefined], [new Date(2026, 0, 3).getTime(), 100]])
      expect(recordedUnits(bars, new Date("2026-01-03T12:00:00Z"), "day", true)).toBe(1)
    })
  })

  it("leaves the days before the first recording out at UTC+13", () => {
    inZone("Pacific/Auckland", () => {
      const period = { from: new Date("2026-01-01T00:00:00Z"), to: new Date("2026-01-04T00:00:00Z"), unit: "day" as const }
      const bars = buildCategoryBars([[]], period, undefined)!

      expect(recordedUnits(bars, new Date("2026-01-02T12:00:00Z"), "day", true)).toBe(2)
    })
  })

  it("adds no empty first day west of UTC", () => {
    const period = { from: new Date("2026-01-02T03:00:00Z"), to: new Date("2026-01-03T03:00:00Z"), unit: "hour" as const }
    const bars = buildCategoryBars([[day("2026-01-02T00:00:00Z", 100)]], period, undefined)!

    expect(bars[0].bytes.get("tv")?.uploaded).toBe(100)
  })
})

describe("bestBar", () => {
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
  it("counts a folded day of an hourly chart as 24 hours", () => {
    const rows: TransferHistoryRow[] = [
      { group: "a.test", start: "2026-01-10T00:00:00Z", width: "day", gapSeconds: 0, uploaded: 500, downloaded: 0, statisticsCrossSeedUploaded: 0 },
      hourRow("2026-01-11T00:00:00Z", "a.test", 7),
    ]
    const bars = buildBars([rows], { from: new Date("2026-01-10T00:00:00Z"), to: new Date("2026-01-11T01:00:00Z") }, "hour")

    expect(recordedUnits(bars, undefined, "hour")).toBe(25)
  })

  it("counts a 25-hour day when the first recording is in its last hour", () => {
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
  it("keeps hourly bars for seven dates across a change of the clock", () => {
    expect(customPeriod(new Date(2026, 9, 26), new Date(2026, 10, 1), new Date(2026, 11, 1)).unit).toBe("hour")
    expect(customPeriod(new Date(2026, 9, 26), new Date(2026, 10, 2), new Date(2026, 11, 1)).unit).toBe("day")
  })

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
    expect([...labelledGaps([300, 340, 360, 364], 365)]).toEqual([300, 364])
  })
})

describe("formatGapLength", () => {
  it("names the units in the language of the page", () => {
    expect(formatGapLength(5400, "en")).toBe("1h 30m")
    expect(formatGapLength(5400, "zh-CN")).toBe("1小时 30分钟")
    expect(formatGapLength(30, "en")).toBe("30s")
  })
})

describe("hourAxis", () => {
  const hourBars = (from: Date, count: number): Pick<StatisticsBar, "start" | "width">[] =>
    Array.from({ length: count }, (_, index) => ({ start: from.getTime() + index * 3_600_000, width: "hour" }))
  const hours = (ticks: number[]) => ticks.map(tick => new Date(tick).getHours())

  it("on 24 hours, shades the hours before midnight, draws a line at midnight, and ticks every 3 hours, or 6 on a phone", () => {
    const bars = hourBars(new Date(2026, 10, 22, 15), 24)

    const axis = hourAxis(bars, false)
    expect(axis.manyDays).toBe(false)
    expect(axis.bands.map(band => [new Date(band.day).getDate(), band.shaded, band.midnightLine])).toEqual([[22, true, false], [23, false, true]])
    expect(hours(axis.ticks)).toEqual([15, 18, 21, 0, 3, 6, 9, 12])
    expect(hours(hourAxis(bars, true).ticks)).toEqual([18, 0, 6, 12])
  })

  it("on 7 days, shades every second date, keeps the shade of a date as the period moves, and ticks every 12 hours, or never on a phone", () => {
    const axis = hourAxis(hourBars(new Date(2026, 10, 16, 15), 168), false)
    const later = hourAxis(hourBars(new Date(2026, 10, 17, 15), 168), false)

    expect(axis.manyDays).toBe(true)
    expect(axis.bands.every((band, index) => index === 0 || band.shaded !== axis.bands[index - 1].shaded)).toBe(true)
    expect(axis.bands.some(band => band.midnightLine)).toBe(false)
    const shadeOf = new Map(axis.bands.map(band => [band.day, band.shaded]))
    for (const band of later.bands.filter(band => shadeOf.has(band.day))) {
      expect(band.shaded).toBe(shadeOf.get(band.day))
    }
    expect(new Set(hours(axis.ticks))).toEqual(new Set([0, 12]))
    expect(hourAxis(hourBars(new Date(2026, 10, 16, 15), 168), true).ticks).toEqual([])
  })

  it("gives a folded day bar its own day and no tick", () => {
    const folded = { start: new Date(2026, 10, 22).getTime(), width: "day" as const }
    const axis = hourAxis([folded, ...hourBars(new Date(2026, 10, 23), 12)], false)

    expect(axis.bands.map(band => new Date(band.day).getDate())).toEqual([22, 23])
    expect(axis.ticks).not.toContain(folded.start)
    expect(hours(axis.ticks)).toEqual([0, 3, 6, 9])
  })
})
