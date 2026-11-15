/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { resolveDashboardSizes } from "@/lib/dashboard-stream"
import { barBytes, buildBars, foldOther, sumBytes, topGroups } from "@/lib/statistics"
import { TRACKER_HEALTH_DISPLAY } from "@/lib/torrent-table/row-display"
import type {
  CacheMetadata,
  InstanceMeta,
  InstanceResponse,
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

// Incognito mode shows this in place of a host or an address.
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

/** The torrent counts of the instance with a tracker problem or an error, in ISSUE_KINDS order. A count of zero is left out. */
export function instanceIssues(d: DashboardInstanceStats): InstanceIssue[] {
  return ISSUE_KINDS.flatMap(kind => {
    const count = d.torrentCounts?.status?.[kind] ?? 0
    return count > 0 ? [{ kind, count, instanceId: d.instance.id, instanceName: d.instance.name }] : []
  })
}

/** The share of the disk that is free, out of the free space and the Unique size of the instance, or undefined without a free space. */
export function freeShare(d: DashboardInstanceStats): number | undefined {
  const free = d.serverState?.free_space_on_disk
  if (free === undefined || free < 0) return undefined
  const total = free + resolveDashboardSizes([d.stats]).totalSize
  return total > 0 ? free / total : undefined
}

// A disk is almost full below this share of free space.
const LOW_DISK_SHARE = 0.05

export function isLowDisk(d: DashboardInstanceStats): boolean {
  return (freeShare(d) ?? 1) < LOW_DISK_SHARE
}

export function connectionStatusOf(d: DashboardInstanceStats): string {
  return (d.instance.connectionStatus ?? d.serverState?.connection_status ?? "").trim().toLowerCase()
}

export function isFirewalled(d: DashboardInstanceStats): boolean {
  return instanceHealth(d) === "ok" && connectionStatusOf(d) === "firewalled"
}

// A ramp of the text color of the theme, one shade for each instance layer, so that the layers stay apart in each theme.
const SHADES = [88, 58, 36, 22, 13]

/** The layer of the instances that share one layer after the instances that have a shade. */
export const OTHER_LAYER = SHADES.length

/** Returns the layer of each instance: its place in instanceIds for the first OTHER_LAYER instances, OTHER_LAYER for the rest. */
export function instanceLayers(instanceIds: number[]): Map<number, number> {
  return new Map(instanceIds.map((id, index) => [id, Math.min(index, OTHER_LAYER)]))
}

/** The fill of an instance layer. The series colors mark Tracker groups only. */
export function layerColor(layer: number): string {
  return layer >= OTHER_LAYER
    ? "color-mix(in oklab, var(--muted-foreground) 30%, transparent)"
    : `color-mix(in oklab, var(--foreground) ${SHADES[layer]}%, transparent)`
}

/** One layer of a chart: the bytes or the speed of each slot, upload and download. */
export interface ChartLayer {
  layer: number
  up: number[]
  down: number[]
}

export interface DayChart {
  hours: { start: number; gapSeconds: number }[]
  layers: ChartLayer[]
}

/**
 * Builds the hours of a chart of the last 24 hours from the rows of each instance, with one layer for each layer of
 * layerOf. An instance that layerOf does not hold goes to OTHER_LAYER. The layers are in layer order, and a layer with
 * no instance is left out.
 */
export function buildDayChart(rowsByInstance: TransferHistoryRow[][], instanceIds: number[], layerOf: Map<number, number>, range: { from: Date; to: Date }): DayChart {
  const bars = buildBars(rowsByInstance, range, "hour")
  const slot = new Map(bars.map((bar, index) => [bar.start, index]))
  const layers = new Map<number, ChartLayer>()
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

/** The speeds of each instance at one time, in bytes per second. */
export interface LiveSample {
  time: number
  speeds: Map<number, { up: number; down: number }>
}

/** Builds the layers of the live chart from the samples, oldest first, in the layers of layerOf. */
export function buildLiveLayers(samples: LiveSample[], layerOf: Map<number, number>): ChartLayer[] {
  const layers = new Map<number, ChartLayer>()
  samples.forEach((sample, index) => {
    for (const [id, speed] of sample.speeds) {
      const layer = layerOf.get(id) ?? OTHER_LAYER
      let into = layers.get(layer)
      if (!into) {
        into = { layer, up: samples.map(() => 0), down: samples.map(() => 0) }
        layers.set(layer, into)
      }
      into.up[index] += speed.up
      into.down[index] += speed.down
    }
  })
  return [...layers.values()].sort((a, b) => a.layer - b.layer)
}

export interface TrackerGroupShare {
  group: string
  uploaded: number
}

export interface TrackerGroupShares {
  top: TrackerGroupShare[]
  // The upload of the other Tracker groups and their count, absent when no other group uploaded.
  other?: { uploaded: number; groups: number }
  uploaded: number
}

/** Returns the Tracker groups with the most upload, in the series order of the Statistics charts, and the rest as Other. The parts add up to the upload. */
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

/** The sum of the layers in each slot, for one half of the chart. */
export function slotTotals(layers: ChartLayer[], half: "up" | "down"): number[] {
  const length = layers[0]?.[half].length ?? 0
  return Array.from({ length }, (_, index) => layers.reduce((sum, layer) => sum + layer[half][index], 0))
}

/** The highest total of each half of the layers. */
export function layerPeaks(layers: ChartLayer[]): { up: number; down: number } {
  return { up: Math.max(0, ...slotTotals(layers, "up")), down: Math.max(0, ...slotTotals(layers, "down")) }
}
