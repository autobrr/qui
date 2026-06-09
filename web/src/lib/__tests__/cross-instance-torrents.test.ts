/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { mergeStreamedCrossInstanceFirstPage, normalizeCrossInstanceTorrents, normalizeStreamedSnapshot, resolveStreamedCrossInstanceTorrents } from "@/lib/cross-instance-torrents"
import type { CrossInstanceTorrent, Torrent, TorrentResponse } from "@/types"

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

describe("normalizeStreamedSnapshot", () => {
  // Guards the flicker regression: the stream handler writes this normalized
  // snapshot into the React Query cache, which the REST-processing effect reads.
  // If the cached snapshot kept snake_case rows, the effect would overwrite the
  // table with blank-Instance rows on every tick.
  it("promotes both cross-instance arrays to camelCase so the cached snapshot is consistent", () => {
    const raw = {
      total: 1,
      cross_instance_torrents: [streamed("a", 3, "seedbox")],
    } as unknown as TorrentResponse

    const result = normalizeStreamedSnapshot(raw)

    expect(result.cross_instance_torrents?.[0]).toEqual(
      expect.objectContaining({ instanceId: 3, instanceName: "seedbox" })
    )
    // Both casings of the array must point at the normalized rows.
    expect(result.crossInstanceTorrents).toBe(result.cross_instance_torrents)
  })

  it("returns the snapshot unchanged when there is nothing to normalize", () => {
    const single = { total: 2, torrents: [] } as unknown as TorrentResponse
    expect(normalizeStreamedSnapshot(single)).toBe(single)
  })
})

describe("mergeStreamedCrossInstanceFirstPage", () => {
  type Snapshot = Pick<TorrentResponse, "total" | "crossInstanceTorrents" | "cross_instance_torrents">

  function snapshot(total: number, ...page0: CrossInstanceTorrent[]): Snapshot {
    return { total, cross_instance_torrents: page0 }
  }

  const hashes = (rows: CrossInstanceTorrent[]) => rows.map(row => row.hash)
  const keys = (rows: CrossInstanceTorrent[]) => rows.map(row => `${row.instanceId}:${row.hash}`)

  // The #1983 regression guard: the aggregated SSE stream only serves page 0, so it
  // must MERGE into the displayed list rather than replace it. Before the fix every
  // snapshot wiped the REST-appended later pages, pinning the unified view to page 0.
  it("preserves pagination-loaded later pages on a steady stream update", () => {
    // prev = page0[a@1, b@1] + page1[c@1, d@1]; the fresh snapshot is still page 0.
    const prev: Torrent[] = [camel("a", 1, "alpha"), camel("b", 1, "alpha"), camel("c", 1, "alpha"), camel("d", 1, "alpha")]
    const merged = mergeStreamedCrossInstanceFirstPage(
      prev,
      snapshot(4, streamed("a", 1, "alpha"), streamed("b", 1, "alpha"))
    )
    expect(hashes(merged)).toEqual(["a", "b", "c", "d"])
  })

  it("keeps cross-seeded same-hash rows on different instances distinct (instanceId+hash identity)", () => {
    // prev = page0[x@1, y@1] + page1[x@2, z@1]; total 4, fresh page 0 = [x@1, y@1].
    // 'x' exists on both instance 1 and instance 2. A hash-only merge would drop the
    // trailing 'x@2' as a duplicate of page-0 'x@1'; the instanceId+hash key keeps it.
    const prev: Torrent[] = [camel("x", 1, "alpha"), camel("y", 1, "alpha"), camel("x", 2, "beta"), camel("z", 1, "alpha")]
    const merged = mergeStreamedCrossInstanceFirstPage(
      prev,
      snapshot(4, streamed("x", 1, "alpha"), streamed("y", 1, "alpha"))
    )
    expect(keys(merged)).toEqual(["1:x", "1:y", "2:x", "1:z"])
  })

  it("de-dupes a cross-instance row that reflowed from a later page up into page 0", () => {
    // prev = page0[a@1, b@1] + page1[x@2, c@1]; total stays 4. b@1 reflows off the page-0
    // window (e.g. after a re-sort) and x@2 reflows up into it, so the fresh page 0 is
    // [a@1, x@2]. x@2 now appears in BOTH the fresh page and the stale trailing slice; the
    // instanceId+hash key must collapse it to one row (and page 0 stays authoritative for
    // its window, so the reflowed-out b@1 is dropped, not resurrected).
    const prev: Torrent[] = [camel("a", 1, "alpha"), camel("b", 1, "alpha"), camel("x", 2, "beta"), camel("c", 1, "alpha")]
    const merged = mergeStreamedCrossInstanceFirstPage(
      prev,
      snapshot(4, streamed("a", 1, "alpha"), streamed("x", 2, "beta"))
    )
    expect(keys(merged)).toEqual(["1:a", "2:x", "1:c"])
  })

  it("normalizes the streamed snake_case page so the Instance column is never blank", () => {
    // Empty prev is also the filter-reset / initial-load path: useTorrentsList resets
    // allTorrents to [] when scope/filters/search/sort change (useTorrentsList.ts), so this
    // doubles as the "first stream payload after a reset" guard.
    const merged = mergeStreamedCrossInstanceFirstPage(
      [],
      snapshot(2, streamed("a", 3, "seedbox"), streamed("b", 7, "home"))
    )
    expect(merged).toHaveLength(2)
    expect(merged.every(t => typeof t.instanceId === "number" && typeof t.instanceName === "string" && t.instanceName.length > 0)).toBe(true)
    expect(merged[0]).toEqual(expect.objectContaining({ hash: "a", instanceId: 3, instanceName: "seedbox" }))
  })

  it("clears the table on a total-0 snapshot no matter how many pages were appended", () => {
    // #1983 is about deep pagination, so prove the clear path holds when prev spans several
    // REST-appended pages: page0[a@1,b@1] + page1[c@1,d@1] + page2[e@1,f@1]. total===0 means
    // the server now reports zero matches, so the whole client list is stale and must be
    // invalidated — pagination depth must not leave orphaned rows behind.
    const prev: Torrent[] = [
      camel("a", 1, "alpha"), camel("b", 1, "alpha"),
      camel("c", 1, "alpha"), camel("d", 1, "alpha"),
      camel("e", 1, "alpha"), camel("f", 1, "alpha"),
    ]
    expect(mergeStreamedCrossInstanceFirstPage(prev, snapshot(0, streamed("a", 1, "alpha")))).toEqual([])
  })
})
