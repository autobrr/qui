/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { applyStreamDelta } from "@/lib/cross-instance-torrents"
import type { Torrent, TorrentResponse } from "@/types"

describe("applyStreamDelta", () => {
  function singleBase(...hashes: string[]): TorrentResponse {
    return {
      torrents: hashes.map(hash => ({ hash, name: `${hash}.iso` })) as unknown as Torrent[],
      total: hashes.length,
    }
  }

  it("reconstructs a single-instance page from an in-place value change", () => {
    const base = singleBase("a", "b", "c")
    const a = base.torrents![0]
    const payload = {
      type: "delta" as const,
      data: { torrents: [{ hash: "b", name: "b-new.iso" }] as unknown as Torrent[], total: 3 },
    }
    const { data, changed } = applyStreamDelta(base, payload, false)
    expect(changed).toBe(true)
    expect(data.torrents!.map(t => t.hash)).toEqual(["a", "b", "c"])
    expect(data.torrents![0]).toBe(a) // unchanged row keeps reference
    expect(data.torrents![1].name).toBe("b-new.iso")
  })

  it("applies a single-instance reorder + add from the order list", () => {
    const base = singleBase("a", "b")
    const payload = {
      type: "delta" as const,
      delta: { order: ["b", "d", "a"], baseVersion: { major: 1, minor: 1 } },
      data: { torrents: [{ hash: "d", name: "d.iso" }] as unknown as Torrent[], total: 3 },
    }
    const { data } = applyStreamDelta(base, payload, false)
    expect(data.torrents!.map(t => t.hash)).toEqual(["b", "d", "a"])
  })

  it("clears the page when a delta drains it to zero rows (present empty order)", () => {
    const base = singleBase("a", "b")
    const payload = {
      type: "delta" as const,
      delta: { order: [] as string[], baseVersion: { major: 1, minor: 1 } },
      data: { torrents: [] as unknown as Torrent[], total: 0 },
    }
    const { data, changed } = applyStreamDelta(base, payload, false)
    expect(changed).toBe(true)
    expect(data.torrents).toEqual([])
  })

  it("reconstructs a cross-instance page and normalizes snake_case changed rows", () => {
    const base: TorrentResponse = {
      crossInstanceTorrents: [{ hash: "a", instanceId: 1, instanceName: "alpha" }, { hash: "a", instanceId: 2, instanceName: "beta" }],
      total: 2,
    } as TorrentResponse
    const payload = {
      type: "delta" as const,
      data: {
        cross_instance_torrents: [{ hash: "a", instance_id: 2, instance_name: "beta" }],
        total: 2,
      } as unknown as TorrentResponse,
    }
    const { data, changed } = applyStreamDelta(base, payload, true)
    expect(changed).toBe(true)
    const rows = data.crossInstanceTorrents!
    expect(rows.map(r => `${r.instanceId}:${r.hash}`)).toEqual(["1:a", "2:a"])
    // The changed instance-2 row is normalized to camelCase instance metadata.
    expect(rows[1].instanceId).toBe(2)
    expect(rows[1].instanceName).toBe("beta")
  })
})

describe("applyStreamDelta", () => {
  it("passes the page through unchanged on an aggregate-only delta", () => {
    const torrents = ["a", "b"].map(hash => ({
      hash,
      name: `${hash}.iso`,
    })) as unknown as Torrent[]
    const base = {
      torrents,
      total: torrents.length,
      counts: { status: { all: 2 }, categories: {}, tags: {}, trackers: {}, total: 2 },
      preferences: { max_ratio: 2 },
    } as unknown as TorrentResponse
    const payload = {
      type: "delta" as const,
      data: {
        torrents: [],
        total: 2,
        stats: { downloading: 1 },
      } as unknown as TorrentResponse,
    }

    const { data, changed } = applyStreamDelta(base, payload, false)

    expect(changed).toBe(false)
    expect(data.torrents).toBe(torrents)
    expect(data.counts).toBe(base.counts)
    expect(data.preferences).toBe(base.preferences)
  })
})
