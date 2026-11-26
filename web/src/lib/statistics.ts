/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatBytes } from "@/lib/utils"
import type { LiveTrackerGroup, TableGrouping, TransferHistoryBytes, TransferHistoryInstance, TransferHistoryRow, TransferHistoryTorrent } from "@/types"

export const BROWSER_TIME_ZONE = ((zone: string) => zone === "Etc/Unknown" ? undefined : zone)(Intl.DateTimeFormat().resolvedOptions().timeZone)

export type PeriodPreset = "24h" | "7d" | "30d" | "1y" | "all"

export type ChartUnit = "hour" | "day"

export type BarWidth = ChartUnit | "week" | "month" | "year"

export interface StatisticsPeriod {
  from?: Date
  to: Date
  unit: ChartUnit
}

export const PERIOD_PRESETS: PeriodPreset[] = ["24h", "7d", "30d", "1y", "all"]

export const TABLE_GROUPINGS: TableGrouping[] = ["tracker", "category", "releaseGroup", "resolution", "source", "contentType"]

const PRESET_DAYS: Record<Exclude<PeriodPreset, "all">, number> = { "24h": 1, "7d": 7, "30d": 30, "1y": 365 }

export const SERIES_COLORS = {
  light: ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4"],
  dark: ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181"],
}

export const SERIES_COUNT = SERIES_COLORS.light.length

const HOUR = 3_600_000

function unitOf(days: number): ChartUnit {
  return days <= 7 ? "hour" : "day"
}

function startOfHour(time: Date): Date {
  return new Date(Math.floor(time.getTime() / HOUR) * HOUR)
}

export function startOfLocalDay(time: Date): Date {
  return new Date(time.getFullYear(), time.getMonth(), time.getDate())
}

export interface DayBand {
  day: number
  first: number
  last: number
  shaded: boolean
  midnightLine: boolean
}

export interface HourAxis {
  ticks: number[]
  bands: DayBand[]
  manyDays: boolean
  narrow: boolean
}

export function hourAxis(bars: Pick<StatisticsBar, "start" | "width">[], narrow: boolean): HourAxis {
  const days: Pick<DayBand, "day" | "first" | "last">[] = []
  for (const { start } of bars) {
    const day = startOfLocalDay(new Date(start)).getTime()
    const last = days.at(-1)
    if (last?.day === day) {
      last.last = start
    } else {
      days.push({ day, first: start, last: start })
    }
  }
  const manyDays = days.length > 2
  const tickHours = manyDays ? (narrow ? undefined : 12) : (narrow ? 6 : 3)
  return {
    ticks: tickHours === undefined ? [] : bars.filter(bar => bar.width === "hour" && new Date(bar.start).getHours() % tickHours === 0).map(bar => bar.start),
    bands: days.map((band, index) => ({
      ...band,
      shaded: manyDays ? daysSinceEpoch(band.day) % 2 === 0 : index < days.length - 1,
      midnightLine: !manyDays && index > 0,
    })),
    manyDays,
    narrow,
  }
}

function daysSinceEpoch(day: number): number {
  const date = new Date(day)
  return Math.round(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / (24 * HOUR))
}

function addLocalDays(day: Date, days: number): Date {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate() + days)
}

export function presetPeriod(preset: PeriodPreset, now: Date): StatisticsPeriod {
  if (preset === "all") {
    return { to: now, unit: "day" }
  }
  const days = PRESET_DAYS[preset]
  const unit = unitOf(days)
  const from = unit === "hour" ? new Date(startOfHour(now).getTime() - (days * 24 - 1) * HOUR) : addLocalDays(startOfLocalDay(now), 1 - days)
  return { from, to: now, unit }
}

export function customPeriod(firstDay: Date, lastDay: Date, now: Date): StatisticsPeriod {
  const from = firstDay
  const end = addLocalDays(lastDay, 1)
  const to = end < now ? end : now
  const shownLastDay = end < now ? lastDay : startOfLocalDay(now)
  return { from, to, unit: unitOf(localDates(firstDay, shownLastDay)) }
}

function localDates(first: Date, last: Date): number {
  return Math.round((Date.UTC(last.getFullYear(), last.getMonth(), last.getDate()) - Date.UTC(first.getFullYear(), first.getMonth(), first.getDate())) / (24 * HOUR)) + 1
}

export function comparisonEnd(period: StatisticsPeriod): Date {
  return startOfHour(period.to)
}

export function previousPeriod(period: StatisticsPeriod): StatisticsPeriod | undefined {
  if (!period.from) {
    return undefined
  }
  const start = startOfHour(period.from)
  const length = comparisonEnd(period).getTime() - start.getTime()
  return length > 0 ? { from: new Date(start.getTime() - length), to: start, unit: period.unit } : undefined
}

export function recordingSince(instances: TransferHistoryInstance[]): Date | undefined {
  const times = instances.map(instance => Date.parse(instance.firstRecordedAt))
  return times.length > 0 ? new Date(Math.min(...times)) : undefined
}

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

export function formatRatio(bytes: Pick<TransferHistoryBytes, "uploaded" | "downloaded">): string | undefined {
  if (bytes.downloaded > 0) {
    return (bytes.uploaded / bytes.downloaded).toFixed(2)
  }
  return bytes.uploaded > 0 ? "∞" : undefined
}

export function formatBuffer(bytes: TransferHistoryBytes): string | undefined {
  if (bytes.uploaded === 0 && bytes.downloaded === 0) {
    return undefined
  }
  return `${bytes.uploaded >= bytes.downloaded ? "+" : "-"}${formatBytes(Math.abs(bytes.uploaded - bytes.downloaded))}`
}

export const HISTORY_WRITE_MS = 5 * 60_000

export function foldOther(bytesByGroup: Map<string, number>, top: string[]): { bytes: number; groups: number } | undefined {
  let bytes = 0
  let groups = 0
  for (const [group, value] of bytesByGroup) {
    if (value > 0 && !top.includes(group)) {
      bytes += value
      groups++
    }
  }
  return groups > 0 ? { bytes, groups } : undefined
}

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

export interface StatisticsBar {
  start: number
  width: BarWidth
  bytes: Map<string, Record<Metric, number>>
  gapSeconds: number
}

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
  if (width === "day" && start.getTime() % (24 * HOUR) === 0) {
    return localDateOfUtc(start.getTime())
  }
  return startOfLocalDay(width === "day" ? new Date(start.getTime() + 12 * HOUR) : start).getTime()
}

export function chartRange(period: StatisticsPeriod, since: Date | undefined): { from: Date; to: Date } | undefined {
  const from = period.from ?? (since && startOfLocalDay(since))
  return from && { from, to: period.to }
}

export function utcDayStart(time: Date): number {
  return Date.UTC(time.getUTCFullYear(), time.getUTCMonth(), time.getUTCDate())
}

export function buildCategoryBars(rowsByInstance: TransferHistoryRow[][], period: StatisticsPeriod, since: Date | undefined): StatisticsBar[] | undefined {
  const start = period.from ?? since
  if (!start) {
    return undefined
  }
  const bars = new Map<number, StatisticsBar>()
  const lastDay = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = utcDayStart(start); day <= lastDay; day += 24 * HOUR) {
    bars.set(day, { start: localDateOfUtc(day), width: "day", bytes: new Map(), gapSeconds: 0 })
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

export function localDateOfUtc(time: number): number {
  const date = new Date(time)
  return new Date(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()).getTime()
}

export function buildTorrentBars(rows: TransferHistoryRow[], period: StatisticsPeriod, since: Date | undefined): StatisticsBar[] | undefined {
  const start = period.from ?? since
  if (!start) {
    return undefined
  }
  const days = new Map<number, StatisticsBar>()
  const lastDay = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = utcDayStart(start); day <= lastDay; day += 24 * HOUR) {
    days.set(day, { start: localDateOfUtc(day), width: "day", bytes: new Map(), gapSeconds: 0 })
  }
  const years = new Map<number, StatisticsBar>()
  for (const row of rows) {
    if (!row.start) {
      continue
    }
    const time = new Date(row.start).getTime()
    let bar = row.width === "year" ? years.get(time) : days.get(utcDayStart(new Date(time)))
    if (!bar && row.width === "year") {
      bar = { start: localDateOfUtc(time), width: "year", bytes: new Map(), gapSeconds: 0 }
      years.set(time, bar)
    }
    if (!bar) {
      continue
    }
    const bytes = bar.bytes.get(row.group) ?? { uploaded: 0, downloaded: 0 }
    bytes.uploaded += row.uploaded
    bytes.downloaded += row.downloaded
    bar.bytes.set(row.group, bytes)
    bar.gapSeconds = Math.max(bar.gapSeconds, row.gapSeconds)
  }
  for (const year of years.values()) {
    const first = Date.UTC(new Date(year.start).getFullYear(), 0, 1)
    const next = Date.UTC(new Date(year.start).getFullYear() + 1, 0, 1)
    for (let day = first; day < next; day += 24 * HOUR) {
      if (days.get(day)?.bytes.size) {
        break
      }
      days.delete(day)
    }
  }
  return [...years.values(), ...days.values()].sort((a, b) => a.start - b.start)
}

export interface TopTorrent extends TransferHistoryBytes, TransferHistoryTorrent {
  instanceId: number
  hash: string
}

export function topTorrents(rowsByInstance: TransferHistoryRow[][], instanceIds: number[], withBaseline: boolean, limit: number): TopTorrent[] {
  const torrents = new Map<string, TopTorrent>()
  rowsByInstance.forEach((rows, index) => {
    const instanceId = instanceIds[index]
    for (const row of rows) {
      if (!row.torrent) {
        continue
      }
      const key = `${instanceId}:${row.group}`
      let torrent = torrents.get(key)
      if (!torrent) {
        torrent = { ...row.torrent, instanceId, hash: row.group, uploaded: 0, downloaded: 0, statisticsCrossSeedUploaded: 0 }
        torrents.set(key, torrent)
      }
      const sum = sumBytes([row], withBaseline)
      torrent.uploaded += sum.uploaded
      torrent.downloaded += sum.downloaded
      torrent.statisticsCrossSeedUploaded += sum.statisticsCrossSeedUploaded
    }
  })
  return [...torrents.values()]
    .sort((a, b) => b.uploaded - a.uploaded || a.instanceId - b.instanceId || a.hash.localeCompare(b.hash))
    .slice(0, limit)
}

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

export function formatGapLength(seconds: number, locale: string): string {
  const parts: [number, string][] = [[Math.floor(seconds / 86_400), "day"], [Math.floor((seconds % 86_400) / 3600), "hour"], [Math.floor((seconds % 3600) / 60), "minute"]]
  const shown = parts.filter(([value]) => value > 0)
  const format = (value: number, unit: string) => new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "narrow" }).format(value)
  return shown.length > 0 ? shown.map(([value, unit]) => format(value, unit)).join(" ") : format(seconds, "second")
}

export interface GroupTotals extends TransferHistoryBytes {
  group: string
  live?: Omit<LiveTrackerGroup, "group">
}

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

export function barBytes(bar: StatisticsBar): Record<Metric, number> {
  const total = { uploaded: 0, downloaded: 0 }
  for (const bytes of bar.bytes.values()) {
    total.uploaded += bytes.uploaded
    total.downloaded += bytes.downloaded
  }
  return total
}

export function recordedUnits(bars: StatisticsBar[], since: Date | undefined, unit: ChartUnit, utcDays = false, to?: Date): number {
  const first = since?.getTime() ?? -Infinity
  const utcDayOf = (time: number) => {
    const date = new Date(time)
    return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate())
  }
  let units = 0
  for (const [index, bar] of bars.entries()) {
    if (bar.width === "year") {
      const next = bars[index + 1]
      const yearEnd = Date.UTC(new Date(bar.start).getFullYear() + 1, 0, 1)
      const end = next ? utcDayOf(next.start) : Math.min(yearEnd, to ? utcDayStart(new Date(to.getTime() - 1)) + 24 * HOUR : yearEnd)
      const from = Math.max(utcDayOf(bar.start), since ? utcDayStart(since) : -Infinity)
      units += Math.max(0, (end - from) / (24 * HOUR))
      continue
    }
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

export function bestBar(bars: StatisticsBar[], unit: BarWidth): { bar: StatisticsBar; uploaded: number } | undefined {
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

const YEAR_VIEW_DAYS = 120

export function showsYearView(period: StatisticsPeriod, since: Date | undefined): boolean {
  const range = chartRange(period, since)
  return range !== undefined && localDates(range.from, new Date(range.to.getTime() - 1)) > YEAR_VIEW_DAYS
}

function joinDays(bars: StatisticsBar[], width: BarWidth, startOf: (day: Date) => Date): StatisticsBar[] {
  const joined = new Map<number, StatisticsBar>()
  const result: StatisticsBar[] = []
  for (const bar of bars) {
    if (bar.width !== "day") {
      result.push(bar)
      continue
    }
    const start = startOf(new Date(bar.start)).getTime()
    let into = joined.get(start)
    if (!into) {
      into = { start, width, bytes: new Map(), gapSeconds: 0 }
      joined.set(start, into)
      result.push(into)
    }
    for (const [group, bytes] of bar.bytes) {
      const sum = into.bytes.get(group) ?? { uploaded: 0, downloaded: 0 }
      sum.uploaded += bytes.uploaded
      sum.downloaded += bytes.downloaded
      into.bytes.set(group, sum)
    }
    into.gapSeconds += bar.gapSeconds
  }
  return result
}

export function weekBars(bars: StatisticsBar[]): StatisticsBar[] {
  return joinDays(bars, "week", day => addLocalDays(day, -daysSinceMonday(day)))
}

export function daysSinceMonday(day: Date): number {
  return (day.getDay() + 6) % 7
}

export function monthBars(bars: StatisticsBar[]): StatisticsBar[] {
  return joinDays(bars, "month", day => new Date(day.getFullYear(), day.getMonth(), 1))
}

export interface YearSummary {
  bestDay?: { start: number; uploaded: number }
  bestMonth?: { start: number; uploaded: number }
  weekendShare?: number
}

export function yearSummary(bars: StatisticsBar[]): YearSummary {
  const day = bestBar(bars, "day")
  const month = bestBar(monthBars(bars), "month")
  let uploaded = 0
  let weekend = 0
  for (const bar of bars) {
    if (bar.width !== "day") {
      continue
    }
    const bytes = barBytes(bar).uploaded
    uploaded += bytes
    const weekday = new Date(bar.start).getDay()
    if (weekday === 0 || weekday === 6) {
      weekend += bytes
    }
  }
  return {
    bestDay: day && { start: day.bar.start, uploaded: day.uploaded },
    bestMonth: month && { start: month.bar.start, uploaded: month.uploaded },
    weekendShare: uploaded > 0 ? weekend / uploaded : undefined,
  }
}

export interface CalendarDay {
  start: number
  uploaded?: number
  gapSeconds: number
}

const CALENDAR_DAYS = 53 * 7

export function calendarDays(bars: StatisticsBar[], to: Date, since: Date | undefined): CalendarDay[] {
  const byStart = new Map(bars.filter(bar => bar.width === "day").map(bar => [bar.start, bar]))
  const last = startOfLocalDay(new Date(to.getTime() - 1))
  const first = since?.getTime() ?? -Infinity
  const days: CalendarDay[] = []
  for (let index = 1 - CALENDAR_DAYS; index <= 0; index++) {
    const day = addLocalDays(last, index)
    const bar = byStart.get(day.getTime())
    if (!bar) {
      continue
    }
    const recorded = addLocalDays(day, 1).getTime() > first
    days.push({ start: day.getTime(), uploaded: recorded ? barBytes(bar).uploaded : undefined, gapSeconds: bar.gapSeconds })
  }
  for (const [index, day] of days.entries()) {
    for (let before = index - 1; before >= 0 && days[before].start >= day.start - day.gapSeconds * 1000; before--) {
      days[before].gapSeconds = Math.max(days[before].gapSeconds, day.gapSeconds)
    }
  }
  return days
}
