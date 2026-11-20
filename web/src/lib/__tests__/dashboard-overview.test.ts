/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { buildLiveLayers, instanceLayers, isLowDisk, layerAt, OTHER_LAYER, type ChartLayer, type DashboardInstanceStats } from "@/lib/dashboard-overview"
import type { InstanceDisk, LiveSamples, ServerState, TorrentStats } from "@/types"

const GiB = 1024 ** 3

const NOW = Date.parse("2026-11-15T12:00:00Z")

/** Samples of one instance, each as [seconds before now, upload, download]. */
function live(instances: Record<number, [number, number, number][]>): LiveSamples {
  return {
    now: new Date(NOW).toISOString(),
    instances: Object.entries(instances).map(([id, samples]) => ({
      instanceId: Number(id),
      samples: samples.map(([ago, up, down]) => ({ time: new Date(NOW - ago * 1000).toISOString(), up, down })),
    })),
  }
}

/** The slots of one half of a layer that hold a value, as slot index to value. */
function filled(layer: ChartLayer | undefined, half: "up" | "down"): Record<number, number> {
  return Object.fromEntries((layer?.[half] ?? []).flatMap((value, index) => value === null ? [] : [[index, value]]))
}

describe("buildLiveLayers", () => {
  it("puts each sample into the 2 second slot of its time, with the last slot ending at now", () => {
    const layers = buildLiveLayers(live({ 1: [[179, 10, 1], [177, 20, 2], [5, 30, 3], [3, 40, 4], [1, 50, 5], [0, 60, 6], [181, 99, 9]] }), instanceLayers([1]))

    expect(layers).toHaveLength(1)
    expect(layers[0].up).toHaveLength(90)
    expect(filled(layers[0], "up")).toEqual({ 0: 10, 1: 20, 87: 30, 88: 40, 89: 60 })
    expect(filled(layers[0], "down")).toEqual({ 0: 1, 1: 2, 87: 3, 88: 4, 89: 6 })
  })

  it("leaves a stretch of more than 3 slots with no sample as a hole, and keeps a zero speed", () => {
    const layers = buildLiveLayers(live({ 1: [[20, 0, 0], [18, 0, 0], [16, 0, 0], [8, 5, 0], [6, 5, 0]] }), instanceLayers([1]))

    expect(layers[0].up.slice(79, 87)).toEqual([0, 0, 0, null, null, null, 5, 5])
    expect(layers[0].up[0]).toBeNull()
  })

  it("hides a single read with no other sample of its instance within 3 slots", () => {
    // 60 s alone; 30 s and 22 s are 4 slots apart; 10 s and 4 s are 3 slots apart.
    const layers = buildLiveLayers(live({ 1: [[60, 1, 0], [30, 2, 0], [22, 3, 0], [10, 4, 0], [4, 5, 0]] }), instanceLayers([1]))

    expect(filled(layers[0], "up")).toEqual({ 84: 4, 85: 4, 86: 4, 87: 5, 88: 5, 89: 5 })
  })

  it("gives no layer to an instance whose only samples are hidden", () => {
    expect(buildLiveLayers(live({ 1: [[60, 1, 0]] }), instanceLayers([1]))).toEqual([])
  })

  // regression: #4192
  it("does not join two samples more than 6 seconds apart, also when their slots are 3 apart", () => {
    // Slots 86 and 89, 7.8 seconds apart.
    expect(buildLiveLayers(live({ 1: [[7.9, 1, 0], [0.1, 2, 0]] }), instanceLayers([1]))).toEqual([])
  })

  it("adds the instances of one layer together, and ignores an instance outside the list", () => {
    const ids = [1, 2, 3, 4, 5, 6, 7]
    const layers = buildLiveLayers(live({ 6: [[3, 10, 0], [1, 10, 0]], 7: [[1, 5, 0], [5, 5, 0]], 9: [[3, 100, 0], [1, 100, 0]] }), instanceLayers(ids))

    expect(layers.map(layer => layer.layer)).toEqual([OTHER_LAYER])
    expect(filled(layers[0], "up")).toEqual({ 87: 5, 88: 15, 89: 15 })
  })

  // regression: #4192
  it("draws an instance that syncs a little slower than each 2 seconds without holes", () => {
    // A sync each 2.2 s, as the stream sync loop does, and each 4.2 s for a slow instance.
    const steady = Array.from({ length: 80 }, (_, index): [number, number, number] => [178 - index * 2.2, 100, 0])
    const slow = Array.from({ length: 43 }, (_, index): [number, number, number] => [178 - index * 4.2, 10, 0])
    const layers = buildLiveLayers(live({ 1: steady, 2: slow }), instanceLayers([1, 2]))

    expect(layers[0].up.slice(1).every(value => value === 100)).toBe(true)
    expect(layers[1].up.slice(1).every(value => value === 10)).toBe(true)
  })

  it("keeps the newer of two samples in one slot", () => {
    const layers = buildLiveLayers(live({ 1: [[3.5, 10, 0], [2.5, 20, 0], [1, 30, 0]] }), instanceLayers([1]))

    expect(filled(layers[0], "up")).toEqual({ 88: 20, 89: 30 })
  })
})

describe("layerAt", () => {
  const layers: ChartLayer[] = [
    { layer: 0, up: [10, 20, null], down: [0, 4, 0] },
    { layer: 1, up: [30, 10, 5], down: [0, 0, 0] },
  ]

  it("finds the layer of a bar in the stack of its slot, and no layer above the stack or on the zero line", () => {
    expect([0, 5, 10, 11, 40, 41].map(value => layerAt(layers, "up", 0, value))).toEqual([undefined, 0, 0, 1, 1, undefined])
  })

  it("follows the straight line of an area between two slots", () => {
    // Halfway between slot 0 and slot 1, layer 0 ends at 15 and layer 1 at 35.
    expect([14, 16, 35, 36].map(value => layerAt(layers, "up", 0.5, value))).toEqual([0, 1, 1, undefined])
  })

  it("finds no layer in a hole, and the next layer above the line to its own base", () => {
    // Between slot 1 and the hole of layer 0 in slot 2, layer 1 goes from 20..30 to 0..5: 10..17.5 halfway.
    expect([5, 12, 18].map(value => layerAt(layers, "up", 1.5, value))).toEqual([undefined, 1, undefined])
  })

  it("reads the half that it gets, and finds no layer outside the slots", () => {
    expect(layerAt(layers, "down", 1, 2)).toBe(0)
    expect(layerAt(layers, "up", -1, 5)).toBeUndefined()
    expect(layerAt(layers, "up", 3, 5)).toBeUndefined()
  })
})

describe("isLowDisk", () => {
  function instance(freeSpace: number, uniqueSize: number, disk: InstanceDisk | null): DashboardInstanceStats {
    return {
      stats: { uniqueSize } as TorrentStats,
      serverState: { free_space_on_disk: freeSpace } as ServerState,
      disk,
    } as DashboardInstanceStats
  }

  // regression: #4328
  it("never warns from an estimate, with no disk or with a disk that qui could not read", () => {
    expect(isLowDisk(instance(4 * GiB, 1000 * GiB, null))).toBe(false)
    expect(isLowDisk(instance(4 * GiB, 1000 * GiB, { freeBytes: null, totalBytes: null }))).toBe(false)
  })

  it("warns when less than 5% of a measured disk is free", () => {
    expect(isLowDisk(instance(40 * GiB, 0, { freeBytes: 40 * GiB, totalBytes: 1000 * GiB }))).toBe(true)
    expect(isLowDisk(instance(50 * GiB, 0, { freeBytes: 50 * GiB, totalBytes: 1000 * GiB }))).toBe(false)
  })

  it("waits until qBittorrent reports its free space", () => {
    expect(isLowDisk(instance(-1, 0, { freeBytes: 40 * GiB, totalBytes: 1000 * GiB }))).toBe(false)
  })
})
