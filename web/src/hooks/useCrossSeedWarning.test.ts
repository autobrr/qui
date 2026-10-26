/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"

import type { LocalCrossSeedMatch, Torrent } from "@/types"

const { getDeleteCrossSeeds } = vi.hoisted(() => ({ getDeleteCrossSeeds: vi.fn() }))
vi.mock("@/lib/api", () => ({ api: { getDeleteCrossSeeds } }))

import { useCrossSeedWarning } from "./useCrossSeedWarning"

const loaded = [{ hash: "aaa", name: "A" }, { hash: "bbb", name: "B" }] as Torrent[]

const match: LocalCrossSeedMatch = {
  instanceId: 1,
  instanceName: "alpha",
  hash: "zzz",
  name: "Z",
  size: 1,
  progress: 1,
  savePath: "/data",
  contentPath: "/data/Z",
  category: "",
  tags: "cross-seed",
  state: "uploading",
  tracker: "",
  matchType: "content_path",
}

afterEach(() => {
  cleanup()
  getDeleteCrossSeeds.mockReset()
})

describe("useCrossSeedWarning", () => {
  // regression: #3341
  it("checks every selected torrent in Select all mode with one request", async () => {
    getDeleteCrossSeeds.mockResolvedValue([match])
    const selectAll = { filters: { status: ["completed"] } as never, search: "show", excludeHashes: ["ccc"], instanceIds: [1] }
    const { result } = renderHook(() => useCrossSeedWarning({ instanceId: 1, torrents: loaded, selectAll }))

    act(() => { result.current.search() })
    await waitFor(() => expect(result.current.searchState).toBe("complete"))

    expect(getDeleteCrossSeeds).toHaveBeenCalledTimes(1)
    expect(getDeleteCrossSeeds).toHaveBeenCalledWith(1, { selectAll: true, ...selectAll }, expect.any(AbortSignal))
    expect(result.current.affectedTorrents.map(t => [t.hash, t.instanceName])).toEqual([["zzz", "alpha"]])
  })

  it("sends the explicit selection as targets", async () => {
    getDeleteCrossSeeds.mockResolvedValue([])
    const { result } = renderHook(() => useCrossSeedWarning({ instanceId: 1, torrents: loaded }))

    act(() => { result.current.search() })
    await waitFor(() => expect(result.current.searchState).toBe("complete"))

    expect(getDeleteCrossSeeds).toHaveBeenCalledWith(1, {
      targets: [{ instanceId: 1, hash: "aaa" }, { instanceId: 1, hash: "bbb" }],
    }, expect.any(AbortSignal))
    expect(result.current.hasWarning).toBe(false)
  })

  // regression: #3341
  it("stops the request on unmount", () => {
    getDeleteCrossSeeds.mockReturnValue(new Promise(() => {}))
    const { result, unmount } = renderHook(() => useCrossSeedWarning({ instanceId: 1, torrents: loaded }))

    act(() => { result.current.search() })
    const signal = getDeleteCrossSeeds.mock.calls[0][2] as AbortSignal
    expect(signal.aborted).toBe(false)
    unmount()
    expect(signal.aborted).toBe(true)
  })

  it("stops the request on reset and stays idle", async () => {
    let reject: (error: Error) => void = () => {}
    getDeleteCrossSeeds.mockReturnValue(new Promise((_, r) => { reject = r }))
    const { result } = renderHook(() => useCrossSeedWarning({ instanceId: 1, torrents: loaded }))

    act(() => { result.current.search() })
    const signal = getDeleteCrossSeeds.mock.calls[0][2] as AbortSignal
    act(() => result.current.reset())
    expect(signal.aborted).toBe(true)

    await act(async () => reject(new DOMException("aborted", "AbortError")))
    expect(result.current.searchState).toBe("idle")
  })

  // A response can finish reading after the abort, because the browser reads
  // the body before it resolves response.json() in a later task.
  // regression: #3341
  it("drops a result that arrives after reset", async () => {
    let resolve: (matches: LocalCrossSeedMatch[]) => void = () => {}
    getDeleteCrossSeeds.mockReturnValue(new Promise(r => { resolve = r }))
    const { result } = renderHook(() => useCrossSeedWarning({ instanceId: 1, torrents: loaded }))

    act(() => { result.current.search() })
    act(() => result.current.reset())
    await act(async () => resolve([match]))

    expect(result.current.searchState).toBe("idle")
    expect(result.current.affectedTorrents).toEqual([])
  })
})
