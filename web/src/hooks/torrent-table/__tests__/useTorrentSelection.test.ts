/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
import { type SelectionRow, type UseTorrentSelectionParams, useTorrentSelection } from "@/hooks/torrent-table/useTorrentSelection"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"

function makeRows(torrents: Torrent[]): SelectionRow[] {
  return torrents.map((original, index) => ({ id: String(index), original }))
}

const DEFAULT_TORRENTS = [
  makeTorrent({ hash: "h0", name: "t0" }),
  makeTorrent({ hash: "h1", name: "t1" }),
  makeTorrent({ hash: "h2", name: "t2" }),
]

function props(torrents: Torrent[], overrides: Partial<UseTorrentSelectionParams> = {}): UseTorrentSelectionParams {
  return {
    sortedTorrents: torrents,
    isReadOnly: false,
    isCrossInstanceEndpoint: false,
    instanceId: 1,
    getVisibleRows: () => makeRows(torrents),
    ...overrides,
  }
}

function render(overrides: Partial<UseTorrentSelectionParams> = {}) {
  return renderHook((p: UseTorrentSelectionParams) => useTorrentSelection(p), {
    initialProps: props(DEFAULT_TORRENTS, overrides),
    wrapper: TorrentSelectionProvider,
  })
}

function pointerDown(shiftKey: boolean) {
  return { shiftKey } as React.PointerEvent<HTMLDivElement>
}

describe("useTorrentSelection — read-only mode", () => {
  it("ignores every selection event", () => {
    const { result } = render({ isReadOnly: true })
    act(() => result.current.handleSelectAll())
    act(() => result.current.selectAll())
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[0], "0", true))
    act(() => result.current.selectOnly(DEFAULT_TORRENTS[1], 1))
    expect(result.current.hasSelection).toBe(false)
  })
})

describe("useTorrentSelection — shift-click anchor", () => {
  it("ticks the range from the anchor to the clicked row", () => {
    const { result } = render()
    result.current.handleCheckboxPointerDown(pointerDown(false))
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[0], "0", true))
    expect([...result.current.selectedKeys]).toEqual(["h0"])

    result.current.handleCheckboxPointerDown(pointerDown(true))
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[2], "2", true))
    expect([...result.current.selectedKeys].sort()).toEqual(["h0", "h1", "h2"])
  })

  it("drops the anchor when the selection clears", () => {
    const { result } = render()
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[0], "0", true))
    expect(result.current.lastSelectedIndexRef.current).toBe(0)
    act(() => result.current.clearSelection())
    expect(result.current.lastSelectedIndexRef.current).toBeNull()
  })
})

describe("useTorrentSelection — rows that are no longer visible", () => {
  it("keeps a tick on a row that is no longer visible", () => {
    const { result, rerender } = render()
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[0], "0", true))
    rerender(props(DEFAULT_TORRENTS.slice(1)))
    expect([...result.current.selectedKeys]).toEqual(["h0"])
  })

  it("keeps an Exclusion on a row that is no longer visible", () => {
    const { result, rerender } = render()
    act(() => result.current.handleSelectAll())
    act(() => result.current.handleCheckboxChange(DEFAULT_TORRENTS[0], "0", false))
    rerender(props(DEFAULT_TORRENTS.slice(1)))
    expect(result.current.isAllSelected).toBe(true)
    expect([...result.current.excludedFromSelectAll]).toEqual(["h0"])
  })
})
