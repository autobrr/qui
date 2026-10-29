/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type UseBulkActionWrappersParams, useBulkActionWrappers } from "@/hooks/torrent-table/useBulkActionWrappers"
import type { ActionTarget } from "@/lib/torrent-selection"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

function ticks(...torrents: Torrent[]): ActionTarget {
  return { kind: "ticks", hashes: torrents.map(t => t.hash), count: torrents.length, previewTorrents: torrents }
}

function makeParams(overrides: Partial<UseBulkActionWrappersParams> = {}): UseBulkActionWrappersParams {
  return {
    actions: {
      deleteCrossSeeds: false,
      handleDelete: vi.fn(),
      handleRenameTorrent: vi.fn(),
    } as unknown as UseBulkActionWrappersParams["actions"],
    actionTarget: ticks(makeTorrent({ hash: "ctx0" })),
    exportTorrents: vi.fn(),
    activeSortField: "added_on",
    activeSortOrder: "desc",
    crossSeedWarning: { affectedTorrents: [] },
    shouldBlockCrossSeeds: false,
    blockCrossSeedHashes: vi.fn(),
    instanceId: 1,
    setDropPayload: vi.fn(),
    onAddTorrentModalChange: vi.fn(),
    ...overrides,
  }
}

function withActions(params: UseBulkActionWrappersParams, actions: Record<string, unknown>): UseBulkActionWrappersParams {
  return { ...params, actions: { ...params.actions, ...actions } }
}

function render(params: UseBulkActionWrappersParams) {
  return renderHook(() => useBulkActionWrappers(params))
}

describe("useBulkActionWrappers — delete (cross-seed aware)", () => {
  it("blocks cross-seed hashes before deleting when shouldBlockCrossSeeds is set", async () => {
    const order: string[] = []
    const params = withActions(makeParams({
      shouldBlockCrossSeeds: true,
      actionTarget: ticks(makeTorrent({ hash: "ctx0", tags: "cross-seed" })),
      blockCrossSeedHashes: vi.fn(async () => { order.push("block") }),
    }), { handleDelete: vi.fn(async () => { order.push("delete") }) })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    expect(order).toEqual(["block", "delete"])
  })

  it("blocks no torrent on another instance that the delete keeps", async () => {
    const params = makeParams({
      shouldBlockCrossSeeds: true,
      actionTarget: ticks(makeTorrent({ hash: "ctx0", tags: "cross-seed" })),
      crossSeedWarning: { affectedTorrents: [{ ...makeTorrent({ hash: "ctx0", tags: "cross-seed" }), instanceId: 2 } as never] },
    })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    const targets = vi.mocked(params.blockCrossSeedHashes).mock.calls[0][1] ?? []
    expect(targets.filter(target => target.instanceId === 2)).toEqual([])
  })

  it("does not block when shouldBlockCrossSeeds is false", async () => {
    const params = makeParams({ shouldBlockCrossSeeds: false })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    expect(vi.mocked(params.blockCrossSeedHashes)).not.toHaveBeenCalled()
    expect(vi.mocked(params.actions.handleDelete)).toHaveBeenCalled()
  })

  it("passes the chosen cross-seeds to handleDelete with the Action target", async () => {
    const actionTarget: ActionTarget = { kind: "selectAll", selectAll: { filters: {} as never, search: "" }, count: 50, previewTorrents: [] }
    const params = withActions(makeParams({
      actionTarget,
      crossSeedWarning: { affectedTorrents: [makeTorrent({ hash: "cs1" })] },
    }), { deleteCrossSeeds: true })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })

    expect(vi.mocked(params.actions.handleDelete)).toHaveBeenCalledWith(actionTarget, [{ instanceId: 1, hash: "cs1" }])
  })
})

describe("useBulkActionWrappers — rename + drop", () => {
  it("renames the torrent of the Action target, and no-ops with no torrent", async () => {
    const withTorrent = makeParams({ actionTarget: ticks(makeTorrent({ hash: "only" })) })
    const a = render(withTorrent)
    await act(async () => { await a.result.current.handleRenameTorrentWrapper("new") })
    expect(vi.mocked(withTorrent.actions.handleRenameTorrent)).toHaveBeenCalledWith("only", "new")

    const empty = makeParams({ actionTarget: ticks() })
    const b = render(empty)
    await act(async () => { await b.result.current.handleRenameTorrentWrapper("new") })
    expect(vi.mocked(empty.actions.handleRenameTorrent)).not.toHaveBeenCalled()
  })

  it("opens the add-torrent modal on drop and clears the payload when consumed", () => {
    const params = makeParams()
    const { result } = render(params)
    act(() => result.current.handleDropPayload({ files: [] } as never))
    expect(vi.mocked(params.setDropPayload)).toHaveBeenCalledWith({ files: [] })
    expect(vi.mocked(params.onAddTorrentModalChange!)).toHaveBeenCalledWith(true)

    act(() => result.current.handleDropPayloadConsumed())
    expect(vi.mocked(params.setDropPayload)).toHaveBeenCalledWith(null)
  })
})
