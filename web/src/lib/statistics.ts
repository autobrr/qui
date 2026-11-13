/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferHistoryBytes, TransferHistoryInstance, TransferHistoryRow } from "@/types"

export type PeriodPreset = "24h" | "7d" | "30d" | "1y" | "all"

export type ChartUnit = "hour" | "day"

/** A period of the Statistics page. Without from, the period has no start. */
export interface StatisticsPeriod {
  from?: Date
  to: Date
  unit: ChartUnit
}

export const PERIOD_PRESETS: PeriodPreset[] = ["24h", "7d", "30d", "1y", "all"]

const PRESET_DAYS: Record<Exclude<PeriodPreset, "all">, number> = { "24h": 1, "7d": 7, "30d": 30, "1y": 365 }

/** The series colors in series order. The other Tracker groups fold into Other. */
export const SERIES_COLORS = {
  light: ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4"],
  dark: ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181"],
}

export const SERIES_COUNT = SERIES_COLORS.light.length

const HOUR = 3_600_000

/** A chart of 7 days or less has a bar for each hour. */
function unitOf(length: number): ChartUnit {
  return length <= 7 * 24 * HOUR ? "hour" : "day"
}

function startOfHour(time: Date): Date {
  return new Date(Math.floor(time.getTime() / HOUR) * HOUR)
}

/** Returns the midnight of the browser timezone that starts the day of time. */
export function startOfLocalDay(time: Date): Date {
  return new Date(time.getFullYear(), time.getMonth(), time.getDate())
}

function addLocalDays(day: Date, days: number): Date {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate() + days)
}

/** Returns the period of a preset that ends at now. A period of days starts at a local midnight. */
export function presetPeriod(preset: PeriodPreset, now: Date): StatisticsPeriod {
  if (preset === "all") {
    return { to: now, unit: "day" }
  }
  const days = PRESET_DAYS[preset]
  const unit = unitOf(days * 24 * HOUR)
  const from = unit === "hour" ? new Date(startOfHour(now).getTime() - (days * 24 - 1) * HOUR) : addLocalDays(startOfLocalDay(now), 1 - days)
  return { from, to: now, unit }
}

/** Returns the period from the first day to the end of the last day, cut at now. Both days are local midnights. */
export function customPeriod(firstDay: Date, lastDay: Date, now: Date): StatisticsPeriod {
  const from = firstDay
  const end = addLocalDays(lastDay, 1)
  const to = end < now ? end : now
  return { from, to, unit: unitOf(to.getTime() - from.getTime()) }
}

/**
 * Returns the end of the part of a period that a comparison uses: the start of
 * its last hour. qui stores whole hours, so the hour that is still running
 * would compare a part of an hour against a whole one.
 */
export function comparisonEnd(period: StatisticsPeriod): Date {
  return startOfHour(period.to)
}

/** Returns the period before, as long as the period up to its comparison end, or undefined for a period with no start. */
export function previousPeriod(period: StatisticsPeriod): StatisticsPeriod | undefined {
  if (!period.from) {
    return undefined
  }
  const length = comparisonEnd(period).getTime() - period.from.getTime()
  return length > 0 ? { from: new Date(period.from.getTime() - length), to: period.from, unit: period.unit } : undefined
}

/** Returns the first recording of the instances, or undefined when no instance has history. */
export function recordingSince(instances: TransferHistoryInstance[]): Date | undefined {
  const times = instances.map(instance => Date.parse(instance.firstRecordedAt))
  return times.length > 0 ? new Date(Math.min(...times)) : undefined
}

/** Reports whether qui recorded each instance from the time from on. */
export function recordedFrom(instances: TransferHistoryInstance[], from: Date): boolean {
  return instances.every(instance => Date.parse(instance.firstRecordedAt) <= from.getTime())
}

export function sumBytes(rows: TransferHistoryRow[], withBaseline: boolean): TransferHistoryBytes {
  const total: TransferHistoryBytes = { uploaded: 0, downloaded: 0, statisticsCrossSeedUploaded: 0 }
  for (const row of rows) {
    for (const bytes of withBaseline && row.baseline ? [row, row.baseline] : [row]) {
      total.uploaded += bytes.uploaded
      total.downloaded += bytes.downloaded
      total.statisticsCrossSeedUploaded += bytes.statisticsCrossSeedUploaded
    }
  }
  return total
}

export type Metric = "uploaded" | "downloaded"

/** Returns the Tracker groups with the most bytes of the metric, at most SERIES_COUNT of them. */
export function topGroups(rows: TransferHistoryRow[], metric: Metric): string[] {
  const totals = new Map<string, number>()
  for (const row of rows) {
    totals.set(row.group, (totals.get(row.group) ?? 0) + row[metric])
  }
  return [...totals]
    .filter(([, bytes]) => bytes > 0)
    .sort(([a, x], [b, y]) => y - x || a.localeCompare(b))
    .slice(0, SERIES_COUNT)
    .map(([group]) => group)
}

/** One bar of the chart. A day bar of an hourly chart is a day that qui folded. */
export interface StatisticsBar {
  start: number
  width: ChartUnit
  bytes: Map<string, Record<Metric, number>>
  gapSeconds: number
}

/**
 * Builds the bars of the chart from the rows of each instance. A day bar starts
 * at a local midnight. In an hourly chart, a row that qui folded into a day
 * stays one bar. The bars cover the chart range with no hole.
 */
export function buildBars(rowsByInstance: TransferHistoryRow[][], range: { from: Date; to: Date }, unit: ChartUnit): StatisticsBar[] {
  const bars = new Map<number, StatisticsBar>()
  const barAt = (start: number) => {
    let bar = bars.get(start)
    if (!bar) {
      bar = { start, width: unit, bytes: new Map(), gapSeconds: 0 }
      bars.set(start, bar)
    }
    return bar
  }

  const first = (unit === "hour" ? startOfHour(range.from) : startOfLocalDay(range.from)).getTime()
  const step = unit === "hour" ? (time: Date) => new Date(time.getTime() + HOUR) : (time: Date) => addLocalDays(time, 1)
  for (let slot = new Date(first); slot < range.to; slot = step(slot)) {
    barAt(slot.getTime())
  }

  const foldedDays: number[] = []
  for (const rows of rowsByInstance) {
    // The rows of each group repeat the start and the gap of their bucket, so each bucket is read once.
    const buckets = new Map<string, StatisticsBar>()
    const gaps = new Map<StatisticsBar, number>()
    for (const row of rows) {
      if (!row.start) {
        continue
      }
      const bucket = row.width + row.start
      let bar = buckets.get(bucket)
      if (!bar) {
        const key = barKey(new Date(row.start), unit)
        // In a timezone with a half-hour offset, the first stored hour starts before the first local day.
        bar = barAt(unit === "day" ? Math.max(first, key) : key)
        if (row.width === "day") {
          bar.width = "day"
          foldedDays.push(bar.start)
        }
        buckets.set(bucket, bar)
        gaps.set(bar, (gaps.get(bar) ?? 0) + row.gapSeconds)
      }
      const bytes = bar.bytes.get(row.group)
      if (bytes) {
        bytes.uploaded += row.uploaded
        bytes.downloaded += row.downloaded
      } else {
        bar.bytes.set(row.group, { uploaded: row.uploaded, downloaded: row.downloaded })
      }
    }
    for (const [bar, gap] of gaps) {
      bar.gapSeconds = Math.max(bar.gapSeconds, gap)
    }
  }

  if (unit === "hour") {
    for (const day of foldedDays) {
      for (let hour = day + HOUR; hour < day + 24 * HOUR; hour += HOUR) {
        if (bars.get(hour)?.bytes.size === 0) {
          bars.delete(hour)
        }
      }
    }
  }
  return [...bars.values()].sort((a, b) => a.start - b.start)
}

function barKey(start: Date, unit: ChartUnit): number {
  return unit === "hour" ? start.getTime() : startOfLocalDay(start).getTime()
}

/** The start of the chart of a period. A period with no start draws from the day of the first recording. */
export function chartRange(period: StatisticsPeriod, since: Date | undefined): { from: Date; to: Date } | undefined {
  const from = period.from ?? (since && startOfLocalDay(since))
  return from && { from, to: period.to }
}

/** Returns about count ticks from 0 past max, on round numbers of the byte unit of max. */
export function byteTicks(max: number, count = 4): number[] {
  if (max <= 0) {
    return [0]
  }
  const unit = 1024 ** Math.floor(Math.log(max) / Math.log(1024))
  const raw = max / unit / count
  const power = 10 ** Math.floor(Math.log10(raw))
  const step = ([1, 2, 2.5, 5, 10].map(factor => factor * power).find(factor => factor >= raw) ?? raw) * unit
  return Array.from({ length: Math.ceil(max / step) + 1 }, (_, index) => index * step)
}
