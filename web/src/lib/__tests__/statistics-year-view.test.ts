/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest"

import { calendarDays, customPeriod, monthBars, presetPeriod, showsYearView, weekBars, yearSummary, type StatisticsBar } from "@/lib/statistics"

const originalTZ = process.env.TZ

beforeAll(() => {
  process.env.TZ = "America/New_York"
})

afterAll(() => {
  process.env.TZ = originalTZ
})

function dayBar(year: number, month: number, date: number, bytes: Record<string, number>, gapSeconds = 0): StatisticsBar {
  return {
    start: new Date(year, month, date).getTime(),
    width: "day",
    gapSeconds,
    bytes: new Map(Object.entries(bytes).map(([group, uploaded]) => [group, { uploaded, downloaded: 1 }])),
  }
}

describe("showsYearView", () => {
  const now = () => new Date(2026, 5, 15, 12)

  it("shows the year view for a range of 121 days and the stacked bars for 120 days", () => {
    expect(showsYearView(customPeriod(new Date(2026, 0, 1), new Date(2026, 4, 1), now()), undefined)).toBe(true)
    expect(showsYearView(customPeriod(new Date(2026, 0, 1), new Date(2026, 3, 30), now()), undefined)).toBe(false)
  })

  it("counts the days of all time from the first recording", () => {
    const all = presetPeriod("all", now())

    expect(showsYearView(all, new Date(2026, 1, 15, 9))).toBe(true)
    expect(showsYearView(all, new Date(2026, 1, 16, 9))).toBe(false)
    expect(showsYearView(presetPeriod("1y", now()), undefined)).toBe(true)
    expect(showsYearView(presetPeriod("30d", now()), undefined)).toBe(false)
  })
})

describe("weekBars", () => {
  it("joins the days of each week from Monday on, and keeps a year bar", () => {
    const year: StatisticsBar = { start: new Date(2025, 0, 1).getTime(), width: "year", gapSeconds: 0, bytes: new Map([["a.test", { uploaded: 9, downloaded: 0 }]]) }
    const bars = [year, dayBar(2026, 0, 4, { "a.test": 1 }), dayBar(2026, 0, 5, { "a.test": 2 }, 60), dayBar(2026, 0, 11, { "a.test": 3, "b.test": 4 }, 120), dayBar(2026, 0, 12, { "b.test": 5 })]
    const weeks = weekBars(bars)

    expect(weeks.map(bar => [bar.width, new Date(bar.start).toDateString()])).toEqual([
      ["year", new Date(2025, 0, 1).toDateString()],
      ["week", new Date(2025, 11, 29).toDateString()],
      ["week", new Date(2026, 0, 5).toDateString()],
      ["week", new Date(2026, 0, 12).toDateString()],
    ])
    expect(weeks.map(bar => Object.fromEntries([...bar.bytes].map(([group, bytes]) => [group, bytes.uploaded])))).toEqual([{ "a.test": 9 }, { "a.test": 1 }, { "a.test": 5, "b.test": 4 }, { "b.test": 5 }])
    expect(weeks[2].gapSeconds).toBe(180)
  })
})

describe("monthBars", () => {
  it("adds up the upload of each Tracker group in each month", () => {
    const months = monthBars([dayBar(2026, 0, 31, { "a.test": 1, "b.test": 2 }), dayBar(2026, 1, 1, { "a.test": 4 }), dayBar(2026, 1, 28, { "a.test": 8, "b.test": 16 })])

    expect(months.map(bar => [bar.width, new Date(bar.start).getMonth()])).toEqual([["month", 0], ["month", 1]])
    expect(months.map(bar => Object.fromEntries([...bar.bytes].map(([group, bytes]) => [group, bytes.uploaded])))).toEqual([{ "a.test": 1, "b.test": 2 }, { "a.test": 12, "b.test": 16 }])
  })
})

describe("yearSummary", () => {
  it("finds the best day and the best month, and the share of the upload on Saturdays and Sundays", () => {
    const bars = [dayBar(2026, 0, 2, { "a.test": 10 }), dayBar(2026, 0, 3, { "a.test": 30 }), dayBar(2026, 0, 4, { "b.test": 20 }), dayBar(2026, 1, 2, { "a.test": 25 }), dayBar(2026, 1, 3, { "a.test": 15 })]
    const summary = yearSummary(bars)

    expect(new Date(summary.bestDay!.start).toDateString()).toBe(new Date(2026, 0, 3).toDateString())
    expect(summary.bestDay!.uploaded).toBe(30)
    expect(new Date(summary.bestMonth!.start).getMonth()).toBe(0)
    expect(summary.bestMonth!.uploaded).toBe(60)
    expect(summary.weekendShare).toBe(0.5)
  })

  it("has no best day and no weekend share without upload", () => {
    expect(yearSummary([dayBar(2026, 0, 3, {})])).toEqual({ bestDay: undefined, bestMonth: undefined, weekendShare: undefined })
  })
})

describe("calendarDays", () => {
  it("has one day for each day of the period in the last 53 weeks, and no upload before the first recording", () => {
    const to = new Date(2026, 5, 15, 12)
    const bars = [dayBar(2026, 5, 13, {}), dayBar(2026, 5, 14, { "a.test": 7 }), dayBar(2026, 5, 15, { "a.test": 3, "b.test": 2 })]
    const days = calendarDays(bars, to, new Date(2026, 5, 14, 8))

    expect(days.map(day => [new Date(day.start).getDate(), day.uploaded])).toEqual([[13, undefined], [14, 7], [15, 5]])
  })

  it("keeps the last 53 weeks of a longer period", () => {
    const bars = Array.from({ length: 400 }, (_, index) => dayBar(2025, 4, 12 + index, {}))
    const days = calendarDays(bars, new Date(2026, 5, 16), undefined)

    expect(days.length).toBe(53 * 7)
    expect(new Date(days[0].start).toDateString()).toBe(new Date(2025, 5, 10).toDateString())
  })

  it("marks each day that a Recording gap reaches back into", () => {
    const gap = (2 * 24 + 1) * 3600
    const bars = [dayBar(2026, 5, 11, { "a.test": 1 }), dayBar(2026, 5, 12, { "a.test": 1 }), dayBar(2026, 5, 13, {}), dayBar(2026, 5, 14, {}), dayBar(2026, 5, 15, { "a.test": 9 }, gap)]
    const days = calendarDays(bars, new Date(2026, 5, 15, 12), undefined)

    expect(days.map(day => day.gapSeconds)).toEqual([0, 0, gap, gap, gap])
  })
})
