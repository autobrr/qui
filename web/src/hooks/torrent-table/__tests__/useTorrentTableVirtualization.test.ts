/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableVirtualization, type UseTorrentTableVirtualizationParams } from "@/hooks/torrent-table/useTorrentTableVirtualization"
import type { Torrent } from "@/types"
import type { Row } from "@tanstack/react-table"
import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Stable virtualizer singleton (TDZ-safe via vi.hoisted) — the whole point of R1
// is that the hook returns THIS instance by reference, not a fresh wrapper.
const { virtualizerMock } = vi.hoisted(() => ({
  virtualizerMock: {
    measure: vi.fn(),
    scrollToOffset: vi.fn(),
    getVirtualItems: () => [],
    getTotalSize: () => 0,
  },
}))

vi.mock("@tanstack/react-virtual", () => ({ useVirtualizer: () => virtualizerMock }))
vi.mock("@/hooks/useKeyboardNavigation", () => ({ useKeyboardNavigation: () => {} }))

function makeRows(n: number): Row<Torrent>[] {
  return Array.from({ length: n }, (_, i) => ({ id: `r${i}` })) as unknown as Row<Torrent>[]
}

function baseProps(over: Partial<UseTorrentTableVirtualizationParams> = {}): UseTorrentTableVirtualizationParams {
  return {
    rows: makeRows(100),
    desktopViewMode: "normal",
    sortedTorrentsLength: 100,
    hasLoadedAll: false,
    isLoadingMore: false,
    backendLoadMore: vi.fn(),
    ...over,
  }
}

beforeEach(() => virtualizerMock.measure.mockClear())
afterEach(() => vi.useRealTimers())

describe("useTorrentTableVirtualization", () => {
  it("returns the virtualizer by reference, stable across rerenders (R1 guard)", () => {
    const { result, rerender } = renderHook((p: UseTorrentTableVirtualizationParams) => useTorrentTableVirtualization(p), {
      initialProps: baseProps(),
    })
    const v1 = result.current.virtualizer

    rerender(baseProps({ desktopViewMode: "dense" }))

    expect(result.current.virtualizer).toBe(v1)
    expect(result.current.virtualizer).toBe(virtualizerMock) // not re-boxed
  })

  it("advances loadedRows by 100, gated by the 100ms re-entrancy lock", () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useTorrentTableVirtualization(baseProps({ sortedTorrentsLength: 1000, rows: makeRows(1000) })))
    expect(result.current.loadedRows).toBe(100)

    act(() => result.current.loadMore())
    expect(result.current.loadedRows).toBe(200)

    // second immediate call is blocked while the lock is held
    act(() => result.current.loadMore())
    expect(result.current.loadedRows).toBe(200)

    act(() => vi.advanceTimersByTime(100))
    act(() => result.current.loadMore())
    expect(result.current.loadedRows).toBe(300)
  })

  it("delegates to backendLoadMore once the local window is exhausted", () => {
    const backendLoadMore = vi.fn()
    const { result } = renderHook(() => useTorrentTableVirtualization(baseProps({
      sortedTorrentsLength: 100, rows: makeRows(100), hasLoadedAll: false, isLoadingMore: false, backendLoadMore,
    })))

    act(() => result.current.loadMore())

    expect(backendLoadMore).toHaveBeenCalledTimes(1)
    expect(result.current.loadedRows).toBe(100) // unchanged — went to the backend branch
  })

  it("clamps safeLoadedRows to the rendered row count", () => {
    const { result } = renderHook(() => useTorrentTableVirtualization(baseProps({ sortedTorrentsLength: 1000, rows: makeRows(40) })))
    expect(result.current.loadedRows).toBe(100)
    expect(result.current.safeLoadedRows).toBe(40)
  })

  it("maps estimatedRowHeight by view mode and remeasures on change", () => {
    const { result, rerender } = renderHook((p: UseTorrentTableVirtualizationParams) => useTorrentTableVirtualization(p), {
      initialProps: baseProps({ desktopViewMode: "normal" }),
    })
    expect(result.current.estimatedRowHeight).toBe(40)

    virtualizerMock.measure.mockClear()
    rerender(baseProps({ desktopViewMode: "compact" }))

    expect(result.current.estimatedRowHeight).toBe(80)
    expect(virtualizerMock.measure).toHaveBeenCalled()
  })

  it("does not shrink loadedRows backward when the dataset transiently drops", () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook((p: UseTorrentTableVirtualizationParams) => useTorrentTableVirtualization(p), {
      initialProps: baseProps({ sortedTorrentsLength: 1000, rows: makeRows(1000) }),
    })
    act(() => result.current.loadMore())
    expect(result.current.loadedRows).toBe(200)

    // A transient server fluctuation drops the dataset length.
    rerender(baseProps({ sortedTorrentsLength: 5, rows: makeRows(1000) }))

    expect(result.current.loadedRows).toBe(200) // not reset down to 5
  })
})
