/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { normalizeCrossInstanceTorrents, resolveStreamedCrossInstanceTorrents } from "@/lib/cross-instance-torrents"
import type { CrossInstanceTorrent, TorrentResponse } from "@/types"

// A streamed cross-instance torrent as it arrives over SSE: the backend's
// CrossInstanceTorrentView serializes instance metadata as snake_case
// (instance_id / instance_name), while the rest of the torrent fields are
// already camelCase. Cast through unknown so the test can model the raw wire
// shape without fighting the camelCase-only public type.
function streamed(hash: string, instanceId: number, instanceName: string): CrossInstanceTorrent {
  return {
    hash,
    name: `${hash}.iso`,
    instance_id: instanceId,
    instance_name: instanceName,
  } as unknown as CrossInstanceTorrent
}

function camel(hash: string, instanceId: number, instanceName: string): CrossInstanceTorrent {
  return {
    hash,
    name: `${hash}.iso`,
    instanceId,
    instanceName,
  } as unknown as CrossInstanceTorrent
}

describe("normalizeCrossInstanceTorrents", () => {
  let errorSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    errorSpy = vi.spyOn(console, "error").mockImplementation(() => {})
  })

  afterEach(() => {
    errorSpy.mockRestore()
  })

  it("returns undefined for nullish input", () => {
    expect(normalizeCrossInstanceTorrents(undefined)).toBeUndefined()
    expect(normalizeCrossInstanceTorrents(null)).toBeUndefined()
  })

  it("promotes snake_case instance metadata to camelCase (the SSE-stream shape)", () => {
    const result = normalizeCrossInstanceTorrents([
      streamed("a", 3, "seedbox"),
      streamed("b", 7, "home"),
    ])

    expect(result).toEqual([
      expect.objectContaining({ hash: "a", instanceId: 3, instanceName: "seedbox" }),
      expect.objectContaining({ hash: "b", instanceId: 7, instanceName: "home" }),
    ])
    // Every row must carry a usable instanceName for the Instance column.
    expect(result?.every(t => typeof t.instanceName === "string" && t.instanceName.length > 0)).toBe(true)
  })

  it("passes already-camelCase torrents through unchanged (REST shape)", () => {
    const input = [camel("a", 1, "alpha")]
    const result = normalizeCrossInstanceTorrents(input)
    expect(result).toEqual(input)
  })

  it("drops torrents missing instance identity instead of emitting blank rows", () => {
    const result = normalizeCrossInstanceTorrents([
      streamed("a", 3, "seedbox"),
      { hash: "b", name: "b.iso" } as unknown as CrossInstanceTorrent,
    ])
    expect(result).toEqual([
      expect.objectContaining({ hash: "a", instanceId: 3, instanceName: "seedbox" }),
    ])
    expect(errorSpy).toHaveBeenCalledTimes(1)
  })
})

describe("resolveStreamedCrossInstanceTorrents", () => {
  function snapshot(
    overrides: Partial<Pick<TorrentResponse, "total" | "crossInstanceTorrents" | "cross_instance_torrents">>
  ): Pick<TorrentResponse, "total" | "crossInstanceTorrents" | "cross_instance_torrents"> {
    return { total: 2, ...overrides }
  }

  // This is the exact regression guard: an SSE snapshot arrives with snake_case
  // instance metadata (cross_instance_torrents, instance_name) and the resolver
  // must hand the table camelCase rows so the Instance column is not blank. The
  // original bug set these raw rows directly, bypassing normalization.
  it("normalizes a streamed snake_case snapshot into camelCase rows", () => {
    const rows = resolveStreamedCrossInstanceTorrents(
      snapshot({
        total: 2,
        cross_instance_torrents: [streamed("a", 3, "seedbox"), streamed("b", 7, "home")],
      })
    )

    expect(rows).toHaveLength(2)
    expect(rows.every(t => typeof t.instanceName === "string" && t.instanceName.length > 0)).toBe(true)
    expect(rows.every(t => typeof t.instanceId === "number" && t.instanceId > 0)).toBe(true)
    expect(rows[0]).toEqual(expect.objectContaining({ hash: "a", instanceId: 3, instanceName: "seedbox" }))
  })

  it("clears the table when the snapshot reports total 0", () => {
    expect(
      resolveStreamedCrossInstanceTorrents(
        snapshot({ total: 0, cross_instance_torrents: [streamed("a", 3, "seedbox")] })
      )
    ).toEqual([])
  })

  it("returns an empty list when no torrents are present", () => {
    expect(resolveStreamedCrossInstanceTorrents(snapshot({ total: 5, cross_instance_torrents: [] }))).toEqual([])
    expect(resolveStreamedCrossInstanceTorrents(snapshot({ total: 5 }))).toEqual([])
  })
})
