/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatBytes } from "@/lib/utils"
import type { LiveTrackerGroup, TransferHistoryBytes, TransferHistoryInstance, TransferHistoryRow } from "@/types"

// A browser that cannot find its zone reports Etc/Unknown. The days are then UTC days.
export const BROWSER_TIME_ZONE = ((zone: string) => zone === "Etc/Unknown" ? undefined : zone)(Intl.DateTimeFormat().resolvedOptions().timeZone)

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

/** A chart of 7 calendar days or less has a bar for each hour. */
function unitOf(days: number): ChartUnit {
  return days <= 7 ? "hour" : "day"
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
  const unit = unitOf(days)
  const from = unit === "hour" ? new Date(startOfHour(now).getTime() - (days * 24 - 1) * HOUR) : addLocalDays(startOfLocalDay(now), 1 - days)
  return { from, to: now, unit }
}

/** Returns the period from the first day to the end of the last day, cut at now. Both days are local midnights. */
export function customPeriod(firstDay: Date, lastDay: Date, now: Date): StatisticsPeriod {
  const from = firstDay
  const end = addLocalDays(lastDay, 1)
  const to = end < now ? end : now
  // The bars cover the range up to now, so a future last day counts as today.
  const shownLastDay = end < now ? lastDay : startOfLocalDay(now)
  // Count dates, not hours: a week with a change of the clock has 167 or 169 hours.
  const days = Math.round((Date.UTC(shownLastDay.getFullYear(), shownLastDay.getMonth(), shownLastDay.getDate()) - Date.UTC(firstDay.getFullYear(), firstDay.getMonth(), firstDay.getDate())) / (24 * HOUR)) + 1
  return { from, to, unit: unitOf(days) }
}

/**
 * Returns the end of the part of a period that a comparison uses: the start of
 * its last hour. qui stores whole hours, so the hour that is still running
 * would compare a part of an hour against a whole one.
 */
export function comparisonEnd(period: StatisticsPeriod): Date {
  return startOfHour(period.to)
}

/**
 * Returns the period before, or undefined for a period with no start. It ends
 * at the hour of the start of the period, so that both periods hold the same
 * whole hours: in a timezone with a half-hour offset, a period starts inside
 * an hour.
 */
export function previousPeriod(period: StatisticsPeriod): StatisticsPeriod | undefined {
  if (!period.from) {
    return undefined
  }
  const start = startOfHour(period.from)
  const length = comparisonEnd(period).getTime() - start.getTime()
  return length > 0 ? { from: new Date(start.getTime() - length), to: start, unit: period.unit } : undefined
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

export function sumBytes(rows: (TransferHistoryBytes & { baseline?: TransferHistoryBytes })[], withBaseline: boolean): TransferHistoryBytes {
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

/** Returns uploaded divided by downloaded, or undefined when nothing moved. */
export function formatRatio(bytes: TransferHistoryBytes): string | undefined {
  if (bytes.downloaded > 0) {
    return (bytes.uploaded / bytes.downloaded).toFixed(2)
  }
  return bytes.uploaded > 0 ? "∞" : undefined
}

/** Returns uploaded minus downloaded with its sign, or undefined when nothing moved. */
export function formatBuffer(bytes: TransferHistoryBytes): string | undefined {
  if (bytes.uploaded === 0 && bytes.downloaded === 0) {
    return undefined
  }
  return `${bytes.uploaded >= bytes.downloaded ? "+" : "-"}${formatBytes(Math.abs(bytes.uploaded - bytes.downloaded))}`
}

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
        const key = barKey(new Date(row.start), row.width, unit)
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

function barKey(start: Date, width: TransferHistoryRow["width"], unit: ChartUnit): number {
  if (unit === "hour") {
    return start.getTime()
  }
  // A day row counts by its middle, so a start a few hours off its midnight stays on its day.
  return startOfLocalDay(width === "day" ? new Date(start.getTime() + 12 * HOUR) : start).getTime()
}

/** The start of the chart of a period. A period with no start draws from the day of the first recording. */
export function chartRange(period: StatisticsPeriod, since: Date | undefined): { from: Date; to: Date } | undefined {
  const from = period.from ?? (since && startOfLocalDay(since))
  return from && { from, to: period.to }
}

function utcDayStart(time: Date): number {
  return Date.UTC(time.getUTCFullYear(), time.getUTCMonth(), time.getUTCDate())
}

/**
 * Builds the bars of the chart of a category: one bar for each UTC day that the period overlaps, because qui stores
 * the category totals for each UTC day. A bar starts at the local midnight of the date of its UTC day, so that its
 * label shows the date of the stored day in each timezone. A period with no start begins at the first recording.
 */
export function buildCategoryBars(rowsByInstance: TransferHistoryRow[][], period: StatisticsPeriod, since: Date | undefined): StatisticsBar[] | undefined {
  const start = period.from ?? since
  if (!start) {
    return undefined
  }
  const bars = new Map<number, StatisticsBar>()
  const lastDay = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = utcDayStart(start); day <= lastDay; day += 24 * HOUR) {
    const date = new Date(day)
    bars.set(day, { start: new Date(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()).getTime(), width: "day", bytes: new Map(), gapSeconds: 0 })
  }
  for (const rows of rowsByInstance) {
    for (const row of rows) {
      const bar = row.start ? bars.get(utcDayStart(new Date(row.start))) : undefined
      if (!bar) {
        continue
      }
      const bytes = bar.bytes.get(row.group) ?? { uploaded: 0, downloaded: 0 }
      bytes.uploaded += row.uploaded
      bytes.downloaded += row.downloaded
      bar.bytes.set(row.group, bytes)
      bar.gapSeconds = Math.max(bar.gapSeconds, row.gapSeconds)
    }
  }
  return [...bars.values()]
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

/**
 * Returns the bars whose Recording gap gets a label: a gap at least a sixth
 * of the chart after the last labelled gap, so that labels do not overlap.
 */
export function labelledGaps(gapBars: number[], barCount: number): Set<number> {
  const spacing = Math.ceil(barCount / 6)
  const labelled = new Set<number>()
  let last = -Infinity
  for (const bar of gapBars) {
    if (bar - last >= spacing) {
      labelled.add(bar)
      last = bar
    }
  }
  return labelled
}

/** Formats the length of a Recording gap in days, hours, and minutes, with the unit names of the locale. */
export function formatGapLength(seconds: number, locale: string): string {
  const parts: [number, string][] = [[Math.floor(seconds / 86_400), "day"], [Math.floor((seconds % 86_400) / 3600), "hour"], [Math.floor((seconds % 3600) / 60), "minute"]]
  const shown = parts.filter(([value]) => value > 0)
  const format = (value: number, unit: string) => new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "narrow" }).format(value)
  return shown.length > 0 ? shown.map(([value, unit]) => format(value, unit)).join(" ") : format(seconds, "second")
}

/** A group of the table: its transfer in the period, and for a Tracker group the torrents that are in qBittorrent now. */
export interface GroupTotals extends TransferHistoryBytes {
  group: string
  live?: Omit<LiveTrackerGroup, "group">
}

/**
 * Adds up the rows of each group over the instances, with the Baseline when withBaseline is set, and joins the live
 * numbers by group. A group with live torrents and no transfer gets a row too. The rows are ordered by upload.
 */
export function groupTotals(rowsByInstance: TransferHistoryRow[][], withBaseline: boolean, liveByInstance?: LiveTrackerGroup[][]): GroupTotals[] {
  const groups = new Map<string, GroupTotals>()
  const groupAt = (group: string) => {
    let totals = groups.get(group)
    if (!totals) {
      totals = { group, uploaded: 0, downloaded: 0, statisticsCrossSeedUploaded: 0 }
      groups.set(group, totals)
    }
    return totals
  }
  for (const row of rowsByInstance.flat()) {
    const totals = groupAt(row.group)
    const sum = sumBytes([row], withBaseline)
    totals.uploaded += sum.uploaded
    totals.downloaded += sum.downloaded
    totals.statisticsCrossSeedUploaded += sum.statisticsCrossSeedUploaded
  }
  for (const live of liveByInstance?.flat() ?? []) {
    const totals = groupAt(live.group)
    totals.live ??= { torrents: 0, size: 0, uploaded: 0 }
    totals.live.torrents += live.torrents
    totals.live.size += live.size
    totals.live.uploaded += live.uploaded
  }
  return [...groups.values()].sort((a, b) => b.uploaded - a.uploaded || b.downloaded - a.downloaded || a.group.localeCompare(b.group))
}

/** Returns the bytes of a bar, over each group. */
export function barBytes(bar: StatisticsBar): Record<Metric, number> {
  const total = { uploaded: 0, downloaded: 0 }
  for (const bytes of bar.bytes.values()) {
    total.uploaded += bytes.uploaded
    total.downloaded += bytes.downloaded
  }
  return total
}

/**
 * Returns the number of units of the chart that its bars cover after the first recording, so that an average leaves
 * out the time before it. A folded day of an hourly chart is a UTC day of 24 hours. With utcDays, each day bar stands
 * for the UTC day of its date, as the bars of buildCategoryBars do.
 */
export function recordedUnits(bars: StatisticsBar[], since: Date | undefined, unit: ChartUnit, utcDays = false): number {
  const first = since?.getTime() ?? -Infinity
  let units = 0
  for (const bar of bars) {
    const start = new Date(bar.start)
    const end = bar.width === "hour" || unit === "hour"
      ? bar.start + (bar.width === "hour" ? HOUR : 24 * HOUR)
      : utcDays ? Date.UTC(start.getFullYear(), start.getMonth(), start.getDate() + 1) : addLocalDays(start, 1).getTime()
    if (end > first) {
      units += unit === "hour" ? (end - bar.start) / HOUR : 1
    }
  }
  return units
}

/**
 * Returns the bar of the chart unit with the most upload, or undefined when no such bar has upload. A folded day of
 * an hourly chart is not an hour, so it is never the best hour.
 */
export function bestBar(bars: StatisticsBar[], unit: ChartUnit): { bar: StatisticsBar; uploaded: number } | undefined {
  let best: { bar: StatisticsBar; uploaded: number } | undefined
  for (const bar of bars) {
    if (bar.width !== unit) {
      continue
    }
    const { uploaded } = barBytes(bar)
    if (uploaded > (best?.uploaded ?? 0)) {
      best = { bar, uploaded }
    }
  }
  return best
}
