/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type SelectionRow, type UseTorrentSelectionParams, useTorrentSelection } from "@/hooks/torrent-table/useTorrentSelection"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

function makeRows(torrents: Torrent[]): SelectionRow[] {
  return torrents.map((original, index) => ({ id: String(index), original }))
}

type Props = {
  sortedTorrents: Torrent[]
  isReadOnly?: boolean
  isCrossInstanceEndpoint?: boolean
  instanceId?: number
  onResetSelection?: (handler?: () => void) => void
  getVisibleRows?: () => SelectionRow[]
}

const DEFAULT_TORRENTS = [
  makeTorrent({ hash: "h0", name: "t0" }),
  makeTorrent({ hash: "h1", name: "t1" }),
  makeTorrent({ hash: "h2", name: "t2" }),
]

function render(overrides: Partial<Props> = {}) {
  const torrents = overrides.sortedTorrents ?? DEFAULT_TORRENTS
  const initialProps: UseTorrentSelectionParams = {
    sortedTorrents: torrents,
    isReadOnly: overrides.isReadOnly ?? false,
    isCrossInstanceEndpoint: overrides.isCrossInstanceEndpoint ?? false,
    instanceId: overrides.instanceId ?? 1,
    onResetSelection: overrides.onResetSelection,
    getVisibleRows: overrides.getVisibleRows ?? (() => makeRows(torrents)),
  }
  return renderHook((p: UseTorrentSelectionParams) => useTorrentSelection(p), { initialProps })
}

describe("useTorrentSelection — select all (Gmail style)", () => {
  it("selects all when nothing is selected", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    expect(result.current.isAllSelected).toBe(true)
    expect(result.current.isSelectAllChecked).toBe(true)
  })

  it("deselects everything when something is already selected", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.handleSelectAll())
    expect(result.current.isAllSelected).toBe(false)
    expect(result.current.rowSelection).toEqual({})
    expect(result.current.excludedFromSelectAll.size).toBe(0)
  })

  it("does nothing in read-only mode", () => {
    const { result } = render({ isReadOnly: true })
    act(() => result.current.handleSelectAll())
    expect(result.current.isAllSelected).toBe(false)
  })
})

describe("useTorrentSelection — row selection", () => {
  it("toggles a single row in regular mode", () => {
    const { result } = render()
    act(() => result.current.handleRowSelection("h0", true, "0"))
    expect(result.current.rowSelection).toEqual({ "0": true })
    expect(result.current.selectedRowIds).toEqual(["0"])
  })

  it("adds to exclusions when deselecting in select-all mode", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.handleRowSelection("h0", false))
    expect(result.current.excludedFromSelectAll.has("h0")).toBe(true)
    expect(result.current.isSelectAllIndeterminate).toBe(true)
    expect(result.current.isSelectAllChecked).toBe(false)
  })

  it("removes from exclusions when re-selecting in select-all mode", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.handleRowSelection("h0", false))
    act(() => result.current.handleRowSelection("h0", true))
    expect(result.current.excludedFromSelectAll.size).toBe(0)
  })

  it("does nothing in read-only mode", () => {
    const { result } = render({ isReadOnly: true })
    act(() => result.current.handleRowSelection("h0", true, "0"))
    expect(result.current.rowSelection).toEqual({})
  })
})

describe("useTorrentSelection — select-all checkbox flags", () => {
  it("is checked only when every visible row is selected (regular mode)", () => {
    const { result } = render()
    act(() => result.current.handleRowSelection("h0", true, "0"))
    expect(result.current.isSelectAllChecked).toBe(false)
    expect(result.current.isSelectAllIndeterminate).toBe(true)

    act(() => {
      result.current.handleRowSelection("h1", true, "1")
      result.current.handleRowSelection("h2", true, "2")
    })
    expect(result.current.isSelectAllChecked).toBe(true)
    expect(result.current.isSelectAllIndeterminate).toBe(false)
  })
})

describe("useTorrentSelection — getSelectionIdentity", () => {
  it("returns the bare hash for a single instance", () => {
    const { result } = render({ isCrossInstanceEndpoint: false, instanceId: 5 })
    expect(result.current.getSelectionIdentity(makeTorrent({ hash: "abc" }))).toBe("abc")
  })

  it("qualifies the hash with the instance id for cross-instance endpoints", () => {
    const { result } = render({ isCrossInstanceEndpoint: true, instanceId: 5 })
    const torrent = { ...makeTorrent({ hash: "abc" }), instanceId: 9 } as Torrent
    expect(result.current.getSelectionIdentity(torrent)).toBe("9:abc")
  })

  it("falls back to the endpoint instance id when the torrent has none", () => {
    const { result } = render({ isCrossInstanceEndpoint: true, instanceId: 5 })
    expect(result.current.getSelectionIdentity(makeTorrent({ hash: "abc" }))).toBe("5:abc")
  })
})

describe("useTorrentSelection — resetSelectionState", () => {
  it("clears all selection state and the range anchor", () => {
    const { result } = render()
    act(() => result.current.handleRowSelection("h0", true, "0"))
    act(() => { result.current.lastSelectedIndexRef.current = 3 })
    act(() => result.current.resetSelectionState())
    expect(result.current.rowSelection).toEqual({})
    expect(result.current.isAllSelected).toBe(false)
    expect(result.current.excludedFromSelectAll.size).toBe(0)
    expect(result.current.lastSelectedIndexRef.current).toBeNull()
  })

  it("registers the reset handler with onResetSelection and clears it on unmount", () => {
    const onResetSelection = vi.fn()
    const { unmount } = render({ onResetSelection })
    expect(onResetSelection).toHaveBeenCalledWith(expect.any(Function))
    unmount()
    expect(onResetSelection).toHaveBeenLastCalledWith(undefined)
  })
})

describe("useTorrentSelection — shift range select", () => {
  it("selects a contiguous range when shift is held on the compact checkbox", () => {
    const { result } = render()
    result.current.handleCompactCheckboxPointerDown({ shiftKey: false } as React.PointerEvent<HTMLDivElement>)
    act(() => result.current.handleCompactCheckboxChange(DEFAULT_TORRENTS[0], "0", true))
    expect(result.current.rowSelection).toEqual({ "0": true })

    result.current.handleCompactCheckboxPointerDown({ shiftKey: true } as React.PointerEvent<HTMLDivElement>)
    act(() => result.current.handleCompactCheckboxChange(DEFAULT_TORRENTS[2], "2", true))
    expect(result.current.rowSelection).toEqual({ "0": true, "1": true, "2": true })
  })
})

describe("useTorrentSelection — validation effects", () => {
  it("prunes a regular selection that references a no-longer-visible row", () => {
    const { result } = render()
    act(() => result.current.setRowSelection({ "999": true }))
    expect(result.current.rowSelection).toEqual({})
  })

  it("keeps a valid regular selection", () => {
    const { result } = render()
    act(() => result.current.handleRowSelection("h0", true, "0"))
    expect(result.current.rowSelection).toEqual({ "0": true })
  })

  it("resets select-all mode when an exclusion is no longer visible", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.setExcludedFromSelectAll(new Set(["not-a-visible-identity"])))
    expect(result.current.isAllSelected).toBe(false)
    expect(result.current.excludedFromSelectAll.size).toBe(0)
  })

  it("keeps select-all mode when the exclusion is a visible identity", () => {
    const { result } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.setExcludedFromSelectAll(new Set(["h0"])))
    expect(result.current.isAllSelected).toBe(true)
    expect(result.current.excludedFromSelectAll.has("h0")).toBe(true)
  })

  it("clears selection when the table becomes empty", () => {
    const { result, rerender } = render()
    act(() => result.current.handleRowSelection("h0", true, "0"))
    rerender({ sortedTorrents: [], getVisibleRows: () => [], isReadOnly: false, isCrossInstanceEndpoint: false, instanceId: 1 })
    expect(result.current.rowSelection).toEqual({})
  })
})
