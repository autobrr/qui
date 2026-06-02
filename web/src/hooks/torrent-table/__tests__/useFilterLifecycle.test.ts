/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useFilterLifecycle, type UseFilterLifecycleParams } from "@/hooks/torrent-table/useFilterLifecycle"
import type { Virtualizer } from "@tanstack/react-virtual"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

function fakeVirtualizer() {
  return { measure: vi.fn(), scrollToOffset: vi.fn() } as unknown as Virtualizer<HTMLDivElement, Element>
}

function makeParams(over: Partial<UseFilterLifecycleParams> = {}): UseFilterLifecycleParams {
  return {
    virtualizer: fakeVirtualizer(),
    sortedTorrentsLength: 500,
    onFilterChange: vi.fn(),
    setColumnFilters: vi.fn(),
    setSorting: vi.fn(),
    setLoadedRows: vi.fn(),
    isCrossSeedFiltering: false,
    columnFiltersLength: 0,
    visibleRowCount: 100,
    loadedRows: 100,
    ...over,
  }
}

const EMPTY_FILTERS = {
  status: [], excludeStatus: [], categories: [], excludeCategories: [],
  tags: [], excludeTags: [], trackers: [], excludeTrackers: [],
}

describe("useFilterLifecycle", () => {
  it("runs the atomic clear-all transaction and settles back to idle", () => {
    const p = makeParams({ sortedTorrentsLength: 500 })
    const { result } = renderHook(() => useFilterLifecycle(p))

    act(() => result.current.clearFiltersAtomically("all"))

    expect(result.current.filterLifecycleState).toBe("idle")
    expect(p.setColumnFilters).toHaveBeenCalledWith([])
    expect(p.setSorting).toHaveBeenCalledWith([])
    expect(p.virtualizer.scrollToOffset).toHaveBeenCalledWith(0)
    expect(p.virtualizer.measure).toHaveBeenCalled()
    expect(p.setLoadedRows).toHaveBeenCalledWith(100) // min(100, 500)
    expect(p.onFilterChange).toHaveBeenCalledWith(EMPTY_FILTERS)
  })

  it("clears columns only without touching the parent filters", () => {
    const p = makeParams()
    const { result } = renderHook(() => useFilterLifecycle(p))

    act(() => result.current.clearFiltersAtomically("columns-only"))

    expect(result.current.filterLifecycleState).toBe("idle")
    expect(p.setColumnFilters).toHaveBeenCalledWith([])
    expect(p.setSorting).toHaveBeenCalledWith([])
    expect(p.onFilterChange).not.toHaveBeenCalled()
  })

  it("keeps clearFiltersAtomically identity stable across rerenders", () => {
    const { result, rerender } = renderHook(() => useFilterLifecycle(makeParams()))
    const fn = result.current.clearFiltersAtomically
    rerender()
    expect(result.current.clearFiltersAtomically).toBe(fn)
  })

  it("bumps loadedRows (no-shrink) when cross-seed column filters clear while idle", () => {
    const setLoadedRows = vi.fn()
    renderHook(() => useFilterLifecycle(makeParams({
      isCrossSeedFiltering: true, columnFiltersLength: 0, sortedTorrentsLength: 30, setLoadedRows,
    })))

    expect(setLoadedRows).toHaveBeenCalledTimes(1)
    const updater = setLoadedRows.mock.calls[0][0] as (prev: number) => number
    expect(updater(10)).toBe(30) // grows toward min(100, 30)
    expect(updater(50)).toBe(50) // never shrinks
  })

  it("clamps loadedRows down to the visible row count while idle", () => {
    const setLoadedRows = vi.fn()
    renderHook(() => useFilterLifecycle(makeParams({ loadedRows: 200, visibleRowCount: 40, setLoadedRows })))

    expect(setLoadedRows).toHaveBeenCalledWith(40)
  })
})
