/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: mock data for the Statistics page prototype (issue #3991). Every name is invented.

const HOUR = 3_600_000
const DAY = 24 * HOUR
const GiB = 1024 ** 3
const TiB = 1024 ** 4

export const HISTORY_DAYS = 371
export const RECORDED_DAYS = 300

export type PeriodKey = "24h" | "7d" | "30d" | "1y" | "all"
export type Dimension = "tracker" | "category" | "releaseGroup" | "resolution" | "source" | "contentType"

export const PERIODS: { key: PeriodKey; label: string; long: string }[] = [
  { key: "24h", label: "24h", long: "Last 24 hours" },
  { key: "7d", label: "7d", long: "Last 7 days" },
  { key: "30d", label: "30d", long: "Last 30 days" },
  { key: "1y", label: "1y", long: "Last year" },
  { key: "all", label: "All", long: "All time" },
]

export const DIMENSIONS: { key: Dimension; label: string }[] = [
  { key: "tracker", label: "Tracker" },
  { key: "category", label: "Category" },
  { key: "releaseGroup", label: "Release group" },
  { key: "resolution", label: "Resolution" },
  { key: "source", label: "Source" },
  { key: "contentType", label: "Content type" },
]

export const INSTANCES = [
  { id: "all", name: "All instances", share: 1, removed: false },
  { id: "atlas", name: "atlas", share: 0.62, removed: false },
  { id: "basalt", name: "basalt", share: 0.3, removed: false },
  { id: "cinder", name: "cinder", share: 0.08, removed: true },
]

interface GroupDef {
  id: string
  name: string
  weight: number
  dl: number
  torrents: number
  size: number
  seeded: number
  baselineUp: number
  baselineDown: number
}

const GROUP_DEFS: GroupDef[] = [
  { id: "aurora", name: "Aurora", weight: 0.31, dl: 0.2, torrents: 2140, size: 31.4 * TiB, seeded: 30.9 * TiB, baselineUp: 9.8 * TiB, baselineDown: 3.1 * TiB },
  { id: "tidepool", name: "Tidepool", weight: 0.22, dl: 0.32, torrents: 1310, size: 22.8 * TiB, seeded: 22.1 * TiB, baselineUp: 7.2 * TiB, baselineDown: 4.4 * TiB },
  { id: "northwind", name: "Northwind", weight: 0.15, dl: 0.45, torrents: 980, size: 14.1 * TiB, seeded: 13.8 * TiB, baselineUp: 4.1 * TiB, baselineDown: 3.6 * TiB },
  { id: "lanternfish", name: "Lanternfish", weight: 0.1, dl: 0.16, torrents: 640, size: 9.6 * TiB, seeded: 9.6 * TiB, baselineUp: 3.3 * TiB, baselineDown: 0.9 * TiB },
  { id: "quillbox", name: "Quillbox", weight: 0.07, dl: 0.55, torrents: 410, size: 5.2 * TiB, seeded: 4.9 * TiB, baselineUp: 1.6 * TiB, baselineDown: 1.4 * TiB },
  { id: "public", name: "Public", weight: 0.06, dl: 0.85, torrents: 220, size: 3.9 * TiB, seeded: 3.1 * TiB, baselineUp: 0.7 * TiB, baselineDown: 2.2 * TiB },
  { id: "saltmarsh", name: "Saltmarsh", weight: 0.04, dl: 0.3, torrents: 190, size: 6.3 * TiB, seeded: 6.3 * TiB, baselineUp: 1.1 * TiB, baselineDown: 0.5 * TiB },
  { id: "emberline", name: "Emberline", weight: 0.03, dl: 0.4, torrents: 120, size: 3.4 * TiB, seeded: 3.2 * TiB, baselineUp: 0.6 * TiB, baselineDown: 0.4 * TiB },
  { id: "foxglove", name: "Foxglove", weight: 0.015, dl: 0.25, torrents: 58, size: 1.9 * TiB, seeded: 1.9 * TiB, baselineUp: 0.3 * TiB, baselineDown: 0.1 * TiB },
  { id: "none", name: "No tracker", weight: 0.005, dl: 0.1, torrents: 9, size: 0.3 * TiB, seeded: 0.2 * TiB, baselineUp: 0.02 * TiB, baselineDown: 0.01 * TiB },
]

const DAILY_TOTAL_UP = 92 * GiB

function mulberry32(seed: number) {
  let a = seed
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

export interface Bucket {
  t: number
  up: number[]
  down: number[]
  recorded: boolean
  gapHours?: number
  added: number
  completed: number
  deleted: number
}

export interface GroupRow {
  key: string
  name: string
  /** Index into the palette, or -1 for a row with no series color. */
  colorIndex: number
  uploaded: number
  downloaded: number
  ratio: number
  buffer: number
  torrents: number
  size: number
  seeded: number
  spark: number[]
}

export interface TorrentRow {
  name: string
  group: string
  size: number
  uploaded: number
  ratio: number
  deleted: boolean
  crossSeed: boolean
  first24h: number
  spark: number[]
}

export interface LibraryPoint {
  t: number
  torrents: number
  totalSize: number
  uniqueSize: number
  freeSpace: number
}

export interface EventRow {
  t: number
  type: "added" | "completed" | "deleted"
  name: string
  group: string
  actor: string
  finalUploaded?: number
  finalRatio?: number
  seededDays?: number
}

export interface Stats {
  period: PeriodKey
  unit: "hour" | "day"
  buckets: Bucket[]
  /** Tracker groups, largest upload first. The first SERIES_COUNT have a series color. */
  groups: GroupRow[]
  totals: { uploaded: number; downloaded: number; ratio: number; buffer: number; prevUploaded: number | null; prevDownloaded: number | null }
  partial: boolean
  recordingSince: number
  crossSeedUploaded: number
  topTorrents: TorrentRow[]
  library: LibraryPoint[]
  events: EventRow[]
  counts: { added: number; completed: number; deleted: number }
  peak: { t: number; value: number }
}

export const SERIES_COUNT = 5

interface World {
  now: number
  days: Bucket[]
  hours: Bucket[]
  library: LibraryPoint[]
}

let world: World | null = null

function startOfLocalDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

function buildWorld(): World {
  const rand = mulberry32(3991)
  const now = Math.floor(Date.now() / HOUR) * HOUR
  const today = startOfLocalDay(now)
  const firstRecorded = HISTORY_DAYS - RECORDED_DAYS

  const hourCount = 8 * 24
  const hours: Bucket[] = []
  for (let i = hourCount - 1; i >= 0; i--) {
    const t = now - i * HOUR
    const hourOfDay = new Date(t).getHours()
    const diurnal = 1 + 0.55 * Math.sin(((hourOfDay - 9) / 24) * 2 * Math.PI)
    const burst = rand() < 0.04 ? 2.5 + rand() * 2.5 : 1
    const up = GROUP_DEFS.map(g => (DAILY_TOTAL_UP / 24) * g.weight * diurnal * burst * (0.45 + 1.1 * rand()))
    const down = GROUP_DEFS.map((g, gi) => up[gi] * g.dl * (rand() < 0.55 ? 0.05 : 0.6 + 2.4 * rand()))
    hours.push({ t, up, down, recorded: true, added: rand() < 0.3 ? 1 + Math.floor(rand() * 3) : 0, completed: rand() < 0.25 ? 1 + Math.floor(rand() * 2) : 0, deleted: rand() < 0.1 ? 1 : 0 })
  }
  const gapHour = hours[hourCount - 62]
  gapHour.gapHours = 3
  gapHour.up = gapHour.up.map(v => v * 3.4)

  const days: Bucket[] = []
  for (let i = 0; i < HISTORY_DAYS; i++) {
    const t = today - (HISTORY_DAYS - 1 - i) * DAY
    const recorded = i >= firstRecorded
    const dow = new Date(t).getDay()
    const weekly = dow === 0 || dow === 6 ? 1.25 : dow === 5 ? 1.1 : 0.92
    const growth = 0.7 + 0.6 * (i / HISTORY_DAYS)
    const season = 1 + 0.18 * Math.sin((i / 58) * 2 * Math.PI)
    const spike = rand() < 0.035 ? 2.2 + rand() * 2.4 : 1
    const up = GROUP_DEFS.map(g => (recorded ? DAILY_TOTAL_UP * g.weight * weekly * growth * season * spike * (0.55 + 0.9 * rand()) : 0))
    const down = GROUP_DEFS.map((g, gi) => up[gi] * g.dl * (rand() < 0.35 ? 0.08 : 0.5 + 1.6 * rand()))
    const added = recorded ? Math.floor(rand() * 9 * growth) + (spike > 1 ? 14 : 0) : 0
    const completed = recorded ? Math.max(0, added - Math.floor(rand() * 3)) : 0
    const deleted = recorded ? (rand() < 0.2 ? Math.floor(rand() * 26) : Math.floor(rand() * 4)) : 0
    days.push({ t, up, down, recorded, added, completed, deleted })
  }
  for (const day of days.slice(-8)) {
    const inDay = hours.filter(h => h.t >= day.t && h.t < day.t + DAY)
    if (inDay.length === 0) continue
    day.up = GROUP_DEFS.map((_, gi) => inDay.reduce((s, h) => s + h.up[gi], 0))
    day.down = GROUP_DEFS.map((_, gi) => inDay.reduce((s, h) => s + h.down[gi], 0))
    day.added = inDay.reduce((s, h) => s + h.added, 0)
    day.completed = inDay.reduce((s, h) => s + h.completed, 0)
    day.deleted = inDay.reduce((s, h) => s + h.deleted, 0)
    const gap = inDay.find(h => h.gapHours)
    if (gap) day.gapHours = gap.gapHours
  }
  const gapDay = days[HISTORY_DAYS - 42]
  gapDay.gapHours = 9
  gapDay.up = gapDay.up.map(v => v * 1.7)

  const library: LibraryPoint[] = []
  let torrents = 5180
  for (let i = 0; i < HISTORY_DAYS; i++) {
    const p = i / (HISTORY_DAYS - 1)
    torrents += days[i].recorded ? days[i].added - days[i].deleted : 2
    const totalSize = (84 + 15 * p + 0.6 * Math.sin(i / 9)) * TiB
    const uniqueSize = (69.5 + 10.5 * p + 0.4 * Math.sin(i / 11)) * TiB
    const freeSpace = (i < 205 ? 9.5 - 7.6 * (i / 205) : 19.5 - 6.8 * ((i - 205) / (HISTORY_DAYS - 205))) * TiB
    library.push({ t: days[i].t, torrents: Math.max(5000, torrents), totalSize, uniqueSize, freeSpace })
  }

  return { now, days, hours, library }
}

function getWorld(): World {
  world ??= buildWorld()
  return world
}

function sum(values: number[]): number {
  return values.reduce((s, v) => s + v, 0)
}

function downsample(values: number[], points: number): number[] {
  if (values.length <= points) return values
  const out: number[] = []
  const step = values.length / points
  for (let i = 0; i < points; i++) {
    out.push(sum(values.slice(Math.floor(i * step), Math.floor((i + 1) * step))))
  }
  return out
}

const TORRENT_DEFS = [
  { name: "Harbor.Lights.S02.2160p.WEB-DL.DDP5.1.DV.H.265-NOVA", group: "Aurora", size: 61.2 * GiB, share: 0.041, deleted: false, crossSeed: false },
  { name: "The.Glass.Orchard.2025.2160p.UHD.BluRay.REMUX.DV.TrueHD.7.1-KITE", group: "Tidepool", size: 78.4 * GiB, share: 0.034, deleted: false, crossSeed: true },
  { name: "Cold.Meridian.S01.1080p.WEB-DL.DDP5.1.H.264-EMBER", group: "Aurora", size: 24.6 * GiB, share: 0.027, deleted: true, crossSeed: false },
  { name: "Paper.Lanterns.2026.1080p.BluRay.x264-TARN", group: "Northwind", size: 13.1 * GiB, share: 0.022, deleted: false, crossSeed: true },
  { name: "Night.Ferry.S03E04.2160p.WEB.H265-WREN", group: "Aurora", size: 6.8 * GiB, share: 0.019, deleted: true, crossSeed: false },
  { name: "Saltwater.Almanac.2024.2160p.WEB-DL.DDP5.1.Atmos.H.265-NOVA", group: "Lanternfish", size: 21.9 * GiB, share: 0.017, deleted: false, crossSeed: true },
  { name: "Quiet.Machines.S01.COMPLETE.1080p.BluRay.x265-LOOM", group: "Tidepool", size: 44.7 * GiB, share: 0.015, deleted: false, crossSeed: false },
  { name: "Low.Tide.Radio.2023.FLAC.24bit.WEB-FERN", group: "Quillbox", size: 1.4 * GiB, share: 0.012, deleted: false, crossSeed: false },
  { name: "The.Cartographers.Daughter.2025.1080p.WEB-DL.DD5.1.H.264-KITE", group: "Public", size: 8.2 * GiB, share: 0.011, deleted: false, crossSeed: false },
  { name: "Signal.Fires.S02.2160p.WEB-DL.DDP5.1.H.265-EMBER", group: "Northwind", size: 52.3 * GiB, share: 0.01, deleted: false, crossSeed: true },
  { name: "Winter.Almanac.2022.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-TARN", group: "Saltmarsh", size: 31.6 * GiB, share: 0.008, deleted: false, crossSeed: true },
  { name: "Orbit.Season.S04E01.1080p.WEB.H264-WREN", group: "Aurora", size: 3.1 * GiB, share: 0.007, deleted: true, crossSeed: false },
]

const EVENT_DEFS: Omit<EventRow, "t">[] = [
  { type: "deleted", name: "Night.Ferry.S03E04.2160p.WEB.H265-WREN", group: "Aurora", actor: "Automation: Ratio 2.0 reached", finalUploaded: 18.4 * GiB, finalRatio: 2.71, seededDays: 12 },
  { type: "added", name: "Night.Ferry.S03E05.2160p.WEB.H265-WREN", group: "Aurora", actor: "autobrr" },
  { type: "completed", name: "Night.Ferry.S03E05.2160p.WEB.H265-WREN", group: "Aurora", actor: "" },
  { type: "deleted", name: "Orbit.Season.S04E01.1080p.WEB.H264-WREN", group: "Aurora", actor: "External: Sonarr", finalUploaded: 9.6 * GiB, finalRatio: 3.1, seededDays: 31 },
  { type: "added", name: "Paper.Lanterns.2026.1080p.BluRay.x264-TARN", group: "Northwind", actor: "Cross-seed" },
  { type: "deleted", name: "Cold.Meridian.S01.1080p.WEB-DL.DDP5.1.H.264-EMBER", group: "Aurora", actor: "You", finalUploaded: 211 * GiB, finalRatio: 8.58, seededDays: 164 },
  { type: "added", name: "Low.Tide.Radio.2023.FLAC.24bit.WEB-FERN", group: "Quillbox", actor: "You" },
  { type: "deleted", name: "Hollow.Pines.2021.720p.WEB.H264-LOOM", group: "Public", actor: "Automation: Unregistered", finalUploaded: 0.4 * GiB, finalRatio: 0.12, seededDays: 3 },
]

const SHARE_TABLES: Record<Exclude<Dimension, "tracker">, [string, number, number][]> = {
  category: [["tv", 0.41, 0.3], ["movies", 0.36, 0.34], ["movies-4k", 0.09, 0.2], ["music", 0.05, 0.4], ["games", 0.03, 0.9], ["books", 0.01, 0.5], ["None", 0.05, 0.6]],
  releaseGroup: [["NOVA", 0.19, 0.2], ["KITE", 0.14, 0.3], ["EMBER", 0.12, 0.25], ["TARN", 0.09, 0.4], ["WREN", 0.08, 0.3], ["LOOM", 0.06, 0.5], ["FERN", 0.03, 0.4], ["212 more", 0.21, 0.4], ["None", 0.08, 0.7]],
  resolution: [["2160p", 0.44, 0.25], ["1080p", 0.39, 0.35], ["720p", 0.06, 0.5], ["SD", 0.02, 0.6], ["None", 0.09, 0.5]],
  source: [["WEB-DL", 0.42, 0.3], ["BluRay", 0.19, 0.3], ["Remux", 0.16, 0.2], ["WEBRip", 0.08, 0.5], ["HDTV", 0.04, 0.6], ["None", 0.11, 0.5]],
  contentType: [["Episode", 0.33, 0.35], ["Movie", 0.31, 0.3], ["Season pack", 0.24, 0.25], ["Music", 0.05, 0.4], ["Game", 0.03, 0.9], ["Book", 0.01, 0.5], ["None", 0.03, 0.6]],
}

export function getStats(period: PeriodKey, instanceId: string): Stats {
  const w = getWorld()
  const share = INSTANCES.find(i => i.id === instanceId)?.share ?? 1
  const scale = (b: Bucket): Bucket => ({ ...b, up: b.up.map(v => v * share), down: b.down.map(v => v * share) })

  const unit: Stats["unit"] = period === "24h" || period === "7d" ? "hour" : "day"
  let buckets: Bucket[]
  let prev: Bucket[] | null = null
  if (period === "24h") {
    buckets = w.hours.slice(-24)
    prev = w.hours.slice(-48, -24)
  } else if (period === "7d") {
    buckets = w.hours.slice(-168)
    prev = w.days.slice(-14, -7)
  } else if (period === "30d") {
    buckets = w.days.slice(-30)
    prev = w.days.slice(-60, -30)
  } else if (period === "1y") {
    buckets = w.days.slice(-365)
  } else {
    buckets = w.days.filter(d => d.recorded)
  }
  buckets = buckets.map(scale)

  const order = GROUP_DEFS.map((g, gi) => ({ g, gi, uploaded: sum(buckets.map(b => b.up[gi])) + (period === "all" ? g.baselineUp * share : 0) }))
    .sort((a, b) => b.uploaded - a.uploaded)

  const groups: GroupRow[] = order.map(({ g, gi, uploaded }, rank) => {
    const downloaded = sum(buckets.map(b => b.down[gi])) + (period === "all" ? g.baselineDown * share : 0)
    return {
      key: g.id,
      name: g.name,
      colorIndex: rank < SERIES_COUNT ? rank : -1,
      uploaded,
      downloaded,
      ratio: downloaded > 0 ? uploaded / downloaded : 0,
      buffer: uploaded - downloaded,
      torrents: Math.round(g.torrents * share),
      size: g.size * share,
      seeded: g.seeded * share,
      spark: downsample(buckets.map(b => b.up[gi]), 28),
    }
  })

  // Buckets carry the groups in rank order so a chart can read index 0..SERIES_COUNT-1 and fold the rest.
  const rankOf = order.map(o => o.gi)
  buckets = buckets.map(b => ({ ...b, up: rankOf.map(gi => b.up[gi]), down: rankOf.map(gi => b.down[gi]) }))

  const uploaded = sum(groups.map(g => g.uploaded))
  const downloaded = sum(groups.map(g => g.downloaded))
  const prevUploaded = prev ? sum(prev.map(b => sum(b.up))) * share : null
  const prevDownloaded = prev ? sum(prev.map(b => sum(b.down))) * share : null

  const tRand = mulberry32(17)
  const topTorrents: TorrentRow[] = TORRENT_DEFS.map(d => {
    const up = uploaded * d.share
    return {
      name: d.name,
      group: d.group,
      size: d.size,
      uploaded: up,
      ratio: d.crossSeed ? 0 : up / d.size + 0.4,
      deleted: d.deleted,
      crossSeed: d.crossSeed,
      first24h: Math.min(up, d.size * (0.6 + 2 * tRand())),
      spark: Array.from({ length: 28 }, (_, i) => Math.max(0, Math.sin(i / (2 + 5 * tRand())) + 0.4 + tRand()) * (d.deleted && i > 18 ? 0 : 1)),
    }
  })

  const libraryDays = period === "24h" || period === "7d" ? 30 : period === "30d" ? 30 : period === "1y" ? 365 : RECORDED_DAYS
  const library = w.library.slice(-libraryDays).map(p => ({ ...p, torrents: Math.round(p.torrents * share), totalSize: p.totalSize * share, uniqueSize: p.uniqueSize * share }))

  const peakBucket = buckets.reduce((best, b) => (sum(b.up) > sum(best.up) ? b : best), buckets[0])

  return {
    period,
    unit,
    buckets,
    groups,
    totals: { uploaded, downloaded, ratio: downloaded > 0 ? uploaded / downloaded : 0, buffer: uploaded - downloaded, prevUploaded, prevDownloaded },
    partial: period === "1y",
    recordingSince: w.days[HISTORY_DAYS - RECORDED_DAYS].t,
    crossSeedUploaded: uploaded * 0.71,
    topTorrents,
    library,
    events: EVENT_DEFS.map((e, i) => ({ ...e, t: w.now - (i * 5 + 1) * HOUR - i * 17 * 60_000 })),
    counts: { added: sum(buckets.map(b => b.added)), completed: sum(buckets.map(b => b.completed)), deleted: sum(buckets.map(b => b.deleted)) },
    peak: { t: peakBucket.t, value: sum(peakBucket.up) },
  }
}

/** Rows for a group-by other than the Tracker group. They add up to the period total. */
export function getBreakdown(stats: Stats, dimension: Dimension): GroupRow[] {
  if (dimension === "tracker") return stats.groups
  const totalSpark = downsample(stats.buckets.map(b => sum(b.up)), 28)
  const rand = mulberry32(dimension.length * 101)
  const liveTorrents = sum(stats.groups.map(g => g.torrents))
  const liveSize = sum(stats.groups.map(g => g.size))
  return SHARE_TABLES[dimension].map(([name, share, dl], i) => {
    const uploaded = stats.totals.uploaded * share
    const downloaded = uploaded * dl
    return {
      key: `${dimension}-${name}`,
      name,
      colorIndex: i < SERIES_COUNT && name !== "None" ? i : -1,
      uploaded,
      downloaded,
      ratio: uploaded / downloaded,
      buffer: uploaded - downloaded,
      torrents: Math.round(liveTorrents * share),
      size: liveSize * share,
      seeded: liveSize * share * 0.97,
      spark: totalSpark.map(v => v * share * (0.5 + rand())),
    }
  })
}

/** The top torrents of one breakdown row. The mock fills the list up to `count` from the other top torrents. */
export function getGroupTorrents(stats: Stats, row: GroupRow, dimension: Dimension, count = 4): TorrentRow[] {
  const own = dimension === "tracker" ? stats.topTorrents.filter(t => t.group === row.name) : []
  const rest = stats.topTorrents.filter(t => !own.includes(t))
  const start = Math.min(own[own.length - 1]?.uploaded ?? Infinity, row.uploaded * 0.09)
  const filler = rest.slice(0, Math.max(0, count - own.length)).map((t, i) => {
    const uploaded = start * 0.72 ** (i + (own.length > 0 ? 1 : 0))
    return { ...t, group: dimension === "tracker" ? row.name : t.group, uploaded, first24h: Math.min(t.first24h, uploaded * 0.4) }
  })
  return [...own, ...filler].slice(0, count)
}

export type DeleteCause = "automation" | "user" | "proxy" | "restore" | "external"

export interface DeleteRow {
  t: number
  name: string
  group: string
  cause: DeleteCause
  /** The rule name or the API key name. Empty for the other causes. */
  causeName: string
  finalUploaded: number
  finalRatio: number
  seededDays: number
}

const DELETE_DEFS: (Omit<DeleteRow, "t"> & { at: number })[] = [
  { at: 0.01, name: "Night.Ferry.S03E04.2160p.WEB.H265-WREN", group: "Aurora", cause: "automation", causeName: "Ratio 2.0 reached", finalUploaded: 18.4 * GiB, finalRatio: 2.71, seededDays: 12 },
  { at: 0.05, name: "Orbit.Season.S04E01.1080p.WEB.H264-WREN", group: "Aurora", cause: "proxy", causeName: "Sonarr", finalUploaded: 9.6 * GiB, finalRatio: 3.1, seededDays: 31 },
  { at: 0.11, name: "Cold.Meridian.S01.1080p.WEB-DL.DDP5.1.H.264-EMBER", group: "Aurora", cause: "user", causeName: "", finalUploaded: 211 * GiB, finalRatio: 8.58, seededDays: 164 },
  { at: 0.19, name: "Hollow.Pines.2021.720p.WEB.H264-LOOM", group: "Public", cause: "automation", causeName: "Unregistered", finalUploaded: 0.4 * GiB, finalRatio: 0.12, seededDays: 3 },
  { at: 0.3, name: "Salt.And.Iron.2024.1080p.BluRay.x264-TARN", group: "Northwind", cause: "automation", causeName: "Seeded 30 days", finalUploaded: 46.2 * GiB, finalRatio: 4.02, seededDays: 30 },
  { at: 0.44, name: "Glass.Harbor.S02E07.1080p.WEB.H264-KITE", group: "Tidepool", cause: "external", causeName: "", finalUploaded: 3.1 * GiB, finalRatio: 0.94, seededDays: 6 },
  { at: 0.61, name: "The.Long.Field.2022.2160p.UHD.BluRay.x265-NOVA", group: "Lanternfish", cause: "automation", causeName: "Ratio 2.0 reached", finalUploaded: 128.7 * GiB, finalRatio: 2.04, seededDays: 58 },
  { at: 0.83, name: "Winter.Atlas.2025.1080p.WEB-DL.DDP5.1.H.264-EMBER", group: "Quillbox", cause: "restore", causeName: "", finalUploaded: 12.9 * GiB, finalRatio: 1.37, seededDays: 21 },
]

/** The latest deletes of the period, newest first. */
export function getDeletes(stats: Stats): DeleteRow[] {
  const end = stats.buckets[stats.buckets.length - 1].t
  const span = end - stats.buckets[0].t
  return DELETE_DEFS.map(({ at, ...d }) => ({ ...d, t: end - span * at }))
}

export interface HeatDay {
  t: number
  value: number
  recorded: boolean
  gapHours?: number
}

/** The last 53 weeks of daily upload, oldest first, starting on a Monday. */
export function getHeatmap(instanceId: string): HeatDay[] {
  const w = getWorld()
  const share = INSTANCES.find(i => i.id === instanceId)?.share ?? 1
  const all = w.days.map(d => ({ t: d.t, value: sum(d.up) * share, recorded: d.recorded, gapHours: d.gapHours }))
  const firstMonday = all.findIndex(d => new Date(d.t).getDay() === 1)
  return all.slice(firstMonday)
}

/** Twelve calendar months of upload by ranked Tracker group, oldest first. */
export function getMonths(stats: Stats): { t: number; up: number[]; recorded: boolean }[] {
  const months = new Map<string, { t: number; up: number[]; recorded: boolean }>()
  for (const b of stats.buckets) {
    const d = new Date(b.t)
    const key = `${d.getFullYear()}-${d.getMonth()}`
    const m = months.get(key) ?? { t: new Date(d.getFullYear(), d.getMonth(), 1).getTime(), up: b.up.map(() => 0), recorded: false }
    m.up = m.up.map((v, i) => v + b.up[i])
    m.recorded ||= b.recorded
    months.set(key, m)
  }
  return [...months.values()].slice(-12)
}

export function sumOf(values: number[]): number {
  return sum(values)
}
