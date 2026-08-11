/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type UseBulkActionWrappersParams, useBulkActionWrappers } from "@/hooks/torrent-table/useBulkActionWrappers"
import { makeTorrent } from "@/test/mockTorrent"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

function makeParams(overrides: Partial<UseBulkActionWrappersParams> = {}): UseBulkActionWrappersParams {
  return {
    handleAction: vi.fn(),
    handleDelete: vi.fn(),
    handleSetComment: vi.fn(),
    handleUpdateTags: vi.fn(),
    handleSetCategory: vi.fn(),
    handleSetLocation: vi.fn(),
    handleRenameTorrent: vi.fn(),
    handleRenameFile: vi.fn(),
    handleRenameFolder: vi.fn(),
    handleRecheck: vi.fn(),
    handleReannounce: vi.fn(),
    handleTmmConfirm: vi.fn(),
    handleSetShareLimit: vi.fn(),
    handleSetSpeedLimits: vi.fn(),
    contextHashes: ["ctx0"],
    contextTorrents: [makeTorrent({ hash: "ctx0" })],
    deleteCrossSeeds: false,
    exportTorrents: vi.fn(),
    isAllSelected: false,
    selectedHashes: ["sel0"],
    selectedTorrents: [makeTorrent({ hash: "sel0" })],
    effectiveSelectionCount: 1,
    selectAllFilters: undefined,
    selectAllExcludeHashes: undefined,
    selectAllExcludedTargets: [],
    filters: undefined,
    effectiveSearch: "query",
    activeSortField: "added_on",
    activeSortOrder: "desc",
    crossSeedWarning: { affectedTorrents: [] },
    shouldBlockCrossSeeds: false,
    blockCrossSeedHashes: vi.fn(),
    isCrossInstanceEndpoint: false,
    instanceIds: undefined,
    instanceId: 1,
    setDropPayload: vi.fn(),
    onAddTorrentModalChange: vi.fn(),
    ...overrides,
  }
}

function render(params: UseBulkActionWrappersParams) {
  return renderHook(() => useBulkActionWrappers(params))
}

describe("useBulkActionWrappers — argument forwarding", () => {
  it("forwards the context selection tuple to handleRecheck", () => {
    const params = makeParams({ contextHashes: ["a", "b"], isAllSelected: false })
    const { result } = render(params)
    act(() => result.current.handleRecheckWrapper())
    expect(vi.mocked(params.handleRecheck)).toHaveBeenCalledWith(
      ["a", "b"], false, undefined, "query", undefined, expect.objectContaining({ clientHashes: ["a", "b"] })
    )
  })

  it("forwards the comment + normalized filters to handleSetComment", () => {
    const params = makeParams()
    const { result } = render(params)
    act(() => result.current.handleSetCommentWrapper("hello"))
    expect(vi.mocked(params.handleSetComment)).toHaveBeenCalledWith(
      "hello", ["ctx0"], false, undefined, "query", undefined, expect.any(Object)
    )
  })

  it("exports with the select-all/sort context", () => {
    const params = makeParams({ isAllSelected: true, effectiveSelectionCount: 42 })
    const { result } = render(params)
    act(() => result.current.handleExportWrapper(["x"], [makeTorrent({ hash: "x" })]))
    expect(vi.mocked(params.exportTorrents)).toHaveBeenCalledWith(expect.objectContaining({
      isAllSelected: true,
      totalSelected: 42,
      search: "query",
      sortField: "added_on",
      sortOrder: "desc",
    }))
  })

  it("forwards instanceIds and excluded targets to export on cross-instance select-all", () => {
    const excludedTargets = [{ instanceId: 2, hash: "x" }]
    const params = makeParams({
      isAllSelected: true,
      isCrossInstanceEndpoint: true,
      instanceIds: [2, 3],
      selectAllExcludedTargets: excludedTargets,
    })
    const { result } = render(params)
    act(() => result.current.handleExportWrapper([], []))
    expect(vi.mocked(params.exportTorrents)).toHaveBeenCalledWith(expect.objectContaining({
      instanceIds: [2, 3],
      excludeTargets: excludedTargets,
    }))
  })

  it("omits instanceIds and excluded targets from export on a single-instance endpoint", () => {
    const params = makeParams({ isAllSelected: true, isCrossInstanceEndpoint: false })
    const { result } = render(params)
    act(() => result.current.handleExportWrapper([], []))
    expect(vi.mocked(params.exportTorrents)).toHaveBeenCalledWith(expect.objectContaining({
      instanceIds: undefined,
      excludeTargets: undefined,
    }))
  })
})

describe("useBulkActionWrappers — runAction", () => {
  it("sends empty hashes and an empty targets when select-all is active", () => {
    const params = makeParams({ isAllSelected: true, effectiveSelectionCount: 99 })
    const { result } = render(params)
    act(() => result.current.runAction("resume" as never, ["h1", "h2"]))
    const call = vi.mocked(params.handleAction).mock.calls[0]
    expect(call[0]).toBe("resume")
    expect(call[1]).toEqual([])
    expect(call[2]).toMatchObject({ clientHashes: ["h1", "h2"], clientCount: 99, targets: undefined })
  })

  it("sends the given hashes when select-all is off", () => {
    const params = makeParams({ isAllSelected: false })
    const { result } = render(params)
    act(() => result.current.runAction("pause" as never, ["h1"]))
    const call = vi.mocked(params.handleAction).mock.calls[0]
    expect(call[1]).toEqual(["h1"])
  })

  it("falls back to selectedHashes when no hashes are passed", () => {
    const params = makeParams({ isAllSelected: false, selectedHashes: ["sel0", "sel1"] })
    const { result } = render(params)
    act(() => result.current.runAction("pause" as never, []))
    const call = vi.mocked(params.handleAction).mock.calls[0]
    expect(call[2]).toMatchObject({ clientHashes: ["sel0", "sel1"] })
  })
})

describe("useBulkActionWrappers — delete (cross-seed aware)", () => {
  it("blocks cross-seed hashes before deleting when shouldBlockCrossSeeds is set", async () => {
    const params = makeParams({
      shouldBlockCrossSeeds: true,
      contextTorrents: [makeTorrent({ hash: "ctx0", tags: "cross-seed" })],
      contextHashes: ["ctx0"],
    })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    expect(vi.mocked(params.blockCrossSeedHashes)).toHaveBeenCalled()
    expect(vi.mocked(params.handleDelete)).toHaveBeenCalled()
  })

  it("does not block when shouldBlockCrossSeeds is false", async () => {
    const params = makeParams({ shouldBlockCrossSeeds: false })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    expect(vi.mocked(params.blockCrossSeedHashes)).not.toHaveBeenCalled()
    expect(vi.mocked(params.handleDelete)).toHaveBeenCalled()
  })

  it("appends affected cross-seed hashes when deleteCrossSeeds is enabled", async () => {
    const params = makeParams({
      deleteCrossSeeds: true,
      contextHashes: ["ctx0"],
      crossSeedWarning: { affectedTorrents: [makeTorrent({ hash: "cs1" })] },
    })
    const { result } = render(params)
    await act(async () => { await result.current.handleDeleteWrapper() })
    const hashes = vi.mocked(params.handleDelete).mock.calls[0][0]
    expect(hashes).toEqual(["ctx0", "cs1"])
  })
})

describe("useBulkActionWrappers — direct category + rename + drop", () => {
  it("uses selectedHashes as the client fallback when handleSetCategoryDirect gets no hashes", () => {
    const params = makeParams({ isAllSelected: false, selectedHashes: ["sel0", "sel1"] })
    const { result } = render(params)
    act(() => result.current.handleSetCategoryDirect("movies", []))
    const meta = vi.mocked(params.handleSetCategory).mock.calls[0][6]
    expect(meta).toMatchObject({ clientHashes: ["sel0", "sel1"] })
  })

  it("renames using the first context hash, and no-ops with no context", async () => {
    const withCtx = makeParams({ contextHashes: ["only"] })
    const a = render(withCtx)
    await act(async () => { await a.result.current.handleRenameTorrentWrapper("new") })
    expect(vi.mocked(withCtx.handleRenameTorrent)).toHaveBeenCalledWith("only", "new")

    const noCtx = makeParams({ contextHashes: [] })
    const b = render(noCtx)
    await act(async () => { await b.result.current.handleRenameTorrentWrapper("new") })
    expect(vi.mocked(noCtx.handleRenameTorrent)).not.toHaveBeenCalled()
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
