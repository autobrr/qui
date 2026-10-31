/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableVirtualization, type UseTorrentTableVirtualizationParams } from "@/hooks/torrent-table/useTorrentTableVirtualization"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import { renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

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

function makeRows(n: number): TorrentRow[] {
  return Array.from({ length: n }, (_, i) => ({ id: `r${i}` })) as unknown as TorrentRow[]
}

function baseProps(over: Partial<UseTorrentTableVirtualizationParams> = {}): UseTorrentTableVirtualizationParams {
  return {
    rows: makeRows(100),
    desktopViewMode: "normal",
    sortedTorrentsLength: 100,
    loadedRows: 100,
    hasMoreRows: true,
    reportNearEnd: vi.fn(),
    hasLoadedAll: false,
    isLoadingMore: false,
    ...over,
  }
}

beforeEach(() => virtualizerMock.measure.mockClear())

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

  it("clamps safeLoadedRows to the rendered row count", () => {
    const { result } = renderHook(() => useTorrentTableVirtualization(baseProps({ sortedTorrentsLength: 1000, rows: makeRows(40) })))
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
})
