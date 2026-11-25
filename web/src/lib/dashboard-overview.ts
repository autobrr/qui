/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { resolveDashboardSizes } from "@/lib/dashboard-stream"
import { barBytes, buildBars, foldOther, sumBytes, topGroups } from "@/lib/statistics"
import { TRACKER_HEALTH_DISPLAY } from "@/lib/torrent-table/row-display"
import type {
  CacheMetadata,
  InstanceDisk,
  InstanceMeta,
  InstanceResponse,
  LiveSamples,
  QBittorrentAppInfo,
  ServerState,
  TorrentCounts,
  TorrentStats,
  TransferHistoryRow
} from "@/types"
import { AlertTriangle, type LucideIcon } from "lucide-react"

export interface DashboardInstanceStats {
  instance: InstanceResponse
  stats: TorrentStats | null
  serverState: ServerState | null
  torrentCounts?: TorrentCounts
  appInfo: QBittorrentAppInfo | null
  altSpeedEnabled: boolean
  isLoading: boolean
  error: unknown
  streamConnected: boolean
  streamError: string | null
  hasLiveDashboardStatsPayload: boolean
  isDashboardStatsFallbackActive: boolean
  cacheMetadata: CacheMetadata | null | undefined
  instanceMeta: InstanceMeta | null
  disk: InstanceDisk | null
}

export type InstanceHealth = "ok" | "loading" | "offline"

export function instanceHealth(d: DashboardInstanceStats): InstanceHealth {
  if (d.isLoading && !d.stats) return "loading"
  if (!d.instance.connected || !d.stats) return "offline"
  return "ok"
}

export type OfflineCause =
  | { kind: "password" }
  | { kind: "error"; message: string }
  | { kind: "noAnswer" }

export const MASK = "••••••"

export function offlineCause(d: DashboardInstanceStats): OfflineCause {
  if (d.instance.hasDecryptionError) return { kind: "password" }
  const message = d.instance.recentErrors?.[0]?.errorMessage || d.streamError
  return message ? { kind: "error", message } : { kind: "noAnswer" }
}

export const ISSUE_KINDS = ["unregistered", "tracker_down", "tracker_error", "errored"] as const

export type IssueKind = typeof ISSUE_KINDS[number]

export interface InstanceIssue {
  kind: IssueKind
  count: number
  instanceId: number
  instanceName: string
}

export const ISSUE_DISPLAY: Record<IssueKind, { icon: LucideIcon; iconClass: string }> = {
  unregistered: TRACKER_HEALTH_DISPLAY.unregistered,
  tracker_down: TRACKER_HEALTH_DISPLAY.tracker_down,
  tracker_error: TRACKER_HEALTH_DISPLAY.tracker_error,
  errored: { icon: AlertTriangle, iconClass: "text-destructive" },
}

export function issueClass(issue: InstanceIssue): string {
  return ISSUE_DISPLAY[issue.kind].iconClass
}

export function instanceIssues(d: DashboardInstanceStats): InstanceIssue[] {
  return ISSUE_KINDS.flatMap(kind => {
    const count = d.torrentCounts?.status?.[kind] ?? 0
    return count > 0 ? [{ kind, count, instanceId: d.instance.id, instanceName: d.instance.name }] : []
  })
}

function diskFreeShare(d: DashboardInstanceStats): number | undefined {
  const { freeBytes, totalBytes } = d.disk ?? {}
  return freeBytes != null && totalBytes ? freeBytes / totalBytes : undefined
}

export function isFreeShareEstimated(d: DashboardInstanceStats): boolean {
  return diskFreeShare(d) === undefined
}

export function freeShare(d: DashboardInstanceStats): number | undefined {
  const measured = diskFreeShare(d)
  if (measured !== undefined) return measured
  const free = d.serverState?.free_space_on_disk
  if (free === undefined || free < 0) return undefined
  const total = free + resolveDashboardSizes([d.stats]).totalSize
  return total > 0 ? free / total : undefined
}

export function allTimeTransfer(statsData: DashboardInstanceStats[]): { uploaded: number; downloaded: number } {
  return {
    uploaded: statsData.reduce((sum, d) => sum + (d.serverState?.alltime_ul ?? 0), 0),
    downloaded: statsData.reduce((sum, d) => sum + (d.serverState?.alltime_dl ?? 0), 0),
  }
}

const LOW_DISK_SHARE = 0.05

export function reportsFreeSpace(d: DashboardInstanceStats): boolean {
  return (d.serverState?.free_space_on_disk ?? -1) >= 0
}

export function isLowDisk(d: DashboardInstanceStats): boolean {
  return reportsFreeSpace(d) && (diskFreeShare(d) ?? 1) < LOW_DISK_SHARE
}

export function connectionStatusOf(d: DashboardInstanceStats): string {
  return (d.instance.connectionStatus ?? d.serverState?.connection_status ?? "").trim().toLowerCase()
}

export function isFirewalled(d: DashboardInstanceStats): boolean {
  return instanceHealth(d) === "ok" && connectionStatusOf(d) === "firewalled"
}

const SHADES = [88, 58, 36, 22, 13]

export const OTHER_LAYER = SHADES.length

export function instanceLayers(instanceIds: number[]): Map<number, number> {
  return new Map(instanceIds.map((id, index) => [id, Math.min(index, OTHER_LAYER)]))
}

export function layerColor(layer: number): string {
  return layer >= OTHER_LAYER
    ? "color-mix(in oklab, var(--muted-foreground) 30%, transparent)"
    : `color-mix(in oklab, var(--foreground) ${SHADES[layer]}%, transparent)`
}

export interface ChartLayer {
  layer: number
  up: (number | null)[]
  down: (number | null)[]
}

export interface DayChart {
  hours: { start: number; gapSeconds: number }[]
  layers: ChartLayer[]
}

export function buildDayChart(rowsByInstance: TransferHistoryRow[][], instanceIds: number[], layerOf: Map<number, number>, range: { from: Date; to: Date }): DayChart {
  const bars = buildBars(rowsByInstance, range, "hour")
  const slot = new Map(bars.map((bar, index) => [bar.start, index]))
  const layers = new Map<number, { layer: number; up: number[]; down: number[] }>()
  rowsByInstance.forEach((rows, index) => {
    const layer = layerOf.get(instanceIds[index]) ?? OTHER_LAYER
    let into = layers.get(layer)
    if (!into) {
      into = { layer, up: bars.map(() => 0), down: bars.map(() => 0) }
      layers.set(layer, into)
    }
    for (const bar of buildBars([rows], range, "hour")) {
      const at = slot.get(bar.start)
      if (at === undefined) continue
      const bytes = barBytes(bar)
      into.up[at] += bytes.uploaded
      into.down[at] += bytes.downloaded
    }
  })
  return {
    hours: bars.map(bar => ({ start: bar.start, gapSeconds: bar.gapSeconds })),
    layers: [...layers.values()].sort((a, b) => a.layer - b.layer),
  }
}

export const LIVE_SLOTS = 90
export const LIVE_SLOT_MS = 2000
const LIVE_JOIN_MS = 3 * LIVE_SLOT_MS

type LiveSample = LiveSamples["instances"][number]["samples"][number]

export function buildLiveLayers(live: LiveSamples, layerOf: Map<number, number>): ChartLayer[] {
  const now = Date.parse(live.now)
  const layers = new Map<number, ChartLayer>()
  for (const { instanceId, samples } of live.instances) {
    const layer = layerOf.get(instanceId)
    if (layer === undefined) continue
    const slots: (LiveSample | undefined)[] = Array(LIVE_SLOTS).fill(undefined)
    for (const sample of samples) {
      const index = LIVE_SLOTS - 1 - Math.floor((now - Date.parse(sample.time)) / LIVE_SLOT_MS)
      if (index >= 0 && index < LIVE_SLOTS) slots[index] = sample
    }
    const placed = slots.flatMap((sample, index) => sample ? [{ index, time: Date.parse(sample.time), sample }] : [])
    const joined = (from: number | undefined, to: number | undefined) => from !== undefined && to !== undefined && to - from <= LIVE_JOIN_MS
    const kept = placed.filter((point, at) => joined(placed[at - 1]?.time, point.time) || joined(point.time, placed[at + 1]?.time))
    if (kept.length === 0) continue
    let into = layers.get(layer)
    if (!into) {
      into = { layer, up: Array(LIVE_SLOTS).fill(null), down: Array(LIVE_SLOTS).fill(null) }
      layers.set(layer, into)
    }
    kept.forEach((point, at) => {
      const next = kept[at + 1]
      const end = next ? (joined(point.time, next.time) ? next.index : point.index + 1) : joined(point.time, now) ? LIVE_SLOTS : point.index + 1
      for (let slot = point.index; slot < end; slot++) {
        into.up[slot] = (into.up[slot] ?? 0) + point.sample.up
        into.down[slot] = (into.down[slot] ?? 0) + point.sample.down
      }
    })
  }
  return [...layers.values()].sort((a, b) => a.layer - b.layer)
}

export interface TrackerGroupShare {
  group: string
  uploaded: number
}

export interface TrackerGroupShares {
  top: TrackerGroupShare[]
  other?: { uploaded: number; groups: number }
  uploaded: number
}

export function trackerGroupShares(rows: TransferHistoryRow[]): TrackerGroupShares {
  const top = topGroups(rows, "uploaded")
  const byGroup = new Map<string, number>()
  for (const row of rows) {
    byGroup.set(row.group, (byGroup.get(row.group) ?? 0) + row.uploaded)
  }
  const other = foldOther(byGroup, top)
  return {
    top: top.map(group => ({ group, uploaded: byGroup.get(group) ?? 0 })),
    other: other && { uploaded: other.bytes, groups: other.groups },
    uploaded: sumBytes(rows, false).uploaded,
  }
}

export function slotTotals(layers: ChartLayer[], half: "up" | "down"): number[] {
  const length = layers[0]?.[half].length ?? 0
  return Array.from({ length }, (_, index) => layers.reduce((sum, layer) => sum + (layer[half][index] ?? 0), 0))
}

export function layerAt(layers: ChartLayer[], half: "up" | "down", at: number, value: number): number | undefined {
  const left = Math.floor(at)
  const right = Math.ceil(at)
  const share = at - left
  let leftSum = 0
  let rightSum = 0
  for (const layer of layers) {
    const leftValue = layer[half][left] ?? null
    const rightValue = layer[half][right] ?? null
    const bottom = leftSum + (rightSum - leftSum) * share
    leftSum += leftValue ?? 0
    rightSum += rightValue ?? 0
    if (leftValue !== null && rightValue !== null && value > bottom && value <= leftSum + (rightSum - leftSum) * share) {
      return layer.layer
    }
  }
  return undefined
}

export function layerPeaks(layers: ChartLayer[]): { up: number; down: number } {
  return { up: Math.max(0, ...slotTotals(layers, "up")), down: Math.max(0, ...slotTotals(layers, "down")) }
}
