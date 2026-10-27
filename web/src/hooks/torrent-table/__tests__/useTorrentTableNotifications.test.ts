/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type UseTorrentTableNotificationsParams, useTorrentTableNotifications } from "@/hooks/torrent-table/useTorrentTableNotifications"
import { makeTorrent } from "@/test/mockTorrent"
import { renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

const COUNTS = { status: {}, categories: {}, tags: {}, trackers: {} } as unknown as UseTorrentTableNotificationsParams["counts"]
// Stable references so the lastMetadataRef no-op suppression can be exercised across rerenders.
const CATEGORIES = {}
const TAGS: string[] = []

function makeParams(overrides: Partial<UseTorrentTableNotificationsParams> = {}): UseTorrentTableNotificationsParams {
  return {
    onFilteredDataUpdate: vi.fn(),
    isLoading: false,
    instanceId: 1,
    counts: COUNTS,
    categories: CATEGORIES,
    tags: TAGS,
    totalCount: 1,
    torrents: [makeTorrent({ hash: "h0" })],
    allowSubcategories: false,
    supportsTrackerHealth: false,
    onSelectionChange: vi.fn(),
    selectedHashes: [],
    selectedTorrents: [],
    effectiveSelectionCount: 0,
    selectAllExcludeHashes: undefined,
    selectAllExcludedTargets: [],
    selectedTotalSize: 0,
    selectAllFilters: undefined,
    ...overrides,
  }
}

describe("useTorrentTableNotifications — onFilteredDataUpdate", () => {
  it("fires with the resolved metadata tuple on first change", () => {
    const params = makeParams()
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    expect(vi.mocked(params.onFilteredDataUpdate!)).toHaveBeenCalledWith(
      params.torrents, 1, COUNTS, {}, [], false, false
    )
  })

  it("emits allowSubcategories verbatim as the useSubcategories arg (no extra gating)", () => {
    const params = makeParams({ allowSubcategories: true })
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    expect(vi.mocked(params.onFilteredDataUpdate!).mock.calls[0][5]).toBe(true)
  })

  it("re-fires when only allowSubcategories changes (e.g. a preference toggle)", () => {
    const cb = vi.fn()
    const { rerender } = renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), {
      initialProps: makeParams({ onFilteredDataUpdate: cb, allowSubcategories: false }),
    })
    expect(cb).toHaveBeenCalledTimes(1)
    rerender(makeParams({ onFilteredDataUpdate: cb, allowSubcategories: true }))
    expect(cb).toHaveBeenCalledTimes(2)
    expect(cb.mock.calls.at(-1)![5]).toBe(true)
  })

  it("is suppressed while loading", () => {
    const params = makeParams({ isLoading: true })
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    expect(vi.mocked(params.onFilteredDataUpdate!)).not.toHaveBeenCalled()
  })

  it("suppresses a no-op re-fire even when the callback identity changes, then fires on real change", () => {
    const cb1 = vi.fn()
    const { rerender } = renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), {
      initialProps: makeParams({ onFilteredDataUpdate: cb1 }),
    })
    expect(cb1).toHaveBeenCalledTimes(1)

    // New callback identity but identical metadata -> suppressed by lastMetadataRef.
    const cb2 = vi.fn()
    rerender(makeParams({ onFilteredDataUpdate: cb2 }))
    expect(cb2).not.toHaveBeenCalled()

    // A real change (totalCount) fires again.
    const cb3 = vi.fn()
    rerender(makeParams({ onFilteredDataUpdate: cb3, totalCount: 2 }))
    expect(cb3).toHaveBeenCalledTimes(1)
  })

  it("re-fires after an instance switch (cache invalidates on instanceId)", () => {
    const cb = vi.fn()
    const { rerender } = renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), {
      initialProps: makeParams({ onFilteredDataUpdate: cb, instanceId: 1 }),
    })
    expect(cb).toHaveBeenCalledTimes(1)
    // instanceId is not a dep, so change a real dep (torrents length) to run the effect;
    // the instanceId mismatch resets the cache and forces a re-fire.
    rerender(makeParams({ onFilteredDataUpdate: cb, instanceId: 2, torrents: [makeTorrent({ hash: "h0" }), makeTorrent({ hash: "h1" })] }))
    expect(cb).toHaveBeenCalledTimes(2)
  })
})

describe("useTorrentTableNotifications — onSelectionChange", () => {
  it("publishes the selection summary", () => {
    const params = makeParams({
      selectedHashes: ["a"],
      selectedTorrents: [makeTorrent({ hash: "a" })],
      effectiveSelectionCount: 1,
      selectedTotalSize: 100,
    })
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    expect(vi.mocked(params.onSelectionChange!)).toHaveBeenCalledWith({
      selectedHashes: ["a"],
      selectedTorrents: params.selectedTorrents,
      totalSelectionCount: 1,
      selectedTotalSize: 100,
      excludeHashes: [],
      excludeTargets: [],
      selectAllFilters: undefined,
    })
  })

  it("carries the excluded targets and selectAllFilters in Select all mode", () => {
    const targets = [{ instanceId: 1, hash: "x" }]
    const filters = { status: [], excludeStatus: [], categories: [], excludeCategories: [], tags: [], excludeTags: [], trackers: [], excludeTrackers: [], expr: "e" } as UseTorrentTableNotificationsParams["selectAllFilters"]
    const params = makeParams({
      selectAllExcludedTargets: targets,
      selectAllFilters: filters,
    })
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    const summary = vi.mocked(params.onSelectionChange!).mock.calls[0][0]
    expect(summary?.excludeTargets).toBe(targets)
    expect(summary?.selectAllFilters).toBe(filters)
  })

  it("fires even while loading (no loading gate on selection)", () => {
    const params = makeParams({ isLoading: true, onFilteredDataUpdate: vi.fn() })
    renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    expect(vi.mocked(params.onSelectionChange!)).toHaveBeenCalled()
  })

  it("publishes null on unmount", () => {
    const params = makeParams()
    const { unmount } = renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    unmount()
    expect(vi.mocked(params.onSelectionChange!)).toHaveBeenLastCalledWith(null)
  })

  it("does not throw when both callbacks are absent", () => {
    const params = makeParams({ onFilteredDataUpdate: undefined, onSelectionChange: undefined })
    expect(() =>
      renderHook((p: UseTorrentTableNotificationsParams) => useTorrentTableNotifications(p), { initialProps: params })
    ).not.toThrow()
  })
})
