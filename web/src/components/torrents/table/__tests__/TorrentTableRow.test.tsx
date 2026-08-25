/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { makeTorrent } from "@/test/mockTorrent"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { Torrent } from "@/types"
import { describe, expect, it } from "vitest"
import { torrentTableRowPropsAreEqual, type TorrentTableRowProps } from "../TorrentTableRow"

function makeRow(torrent: Torrent, id = torrent.hash): TorrentRow {
  return { id, original: torrent } as TorrentRow
}

function makeProps(overrides: Partial<TorrentTableRowProps> = {}): TorrentTableRowProps {
  const torrent = makeTorrent({ hash: "a" })
  return {
    row: makeRow(torrent),
    virtualIndex: 0,
    virtualStart: 0,
    virtualSize: 40,
    isSelected: false,
    isRowSelected: false,
    desktopViewMode: "normal",
    minTableWidth: 800,
    columns: [],
    columnSizing: {},
    columnVisibility: {},
    columnOrder: [],
    menu: {} as TorrentTableRowProps["menu"],
    compact: {} as TorrentTableRowProps["compact"],
    onRowClick: () => {},
    onRowContextMenu: () => {},
    ...overrides,
  }
}

describe("torrentTableRowPropsAreEqual", () => {
  it("treats a rebuilt Row wrapper around the same torrent object as equal", () => {
    const prev = makeProps()
    // TanStack rebuilds the Row wrapper on every data-array change; only the
    // underlying torrent identity may decide whether the row re-renders.
    const next = { ...prev, row: makeRow(prev.row.original, prev.row.id) }
    expect(torrentTableRowPropsAreEqual(prev, next)).toBe(true)
  })

  it("re-renders when the torrent object identity changes, even for equal values", () => {
    const prev = makeProps()
    const next = { ...prev, row: makeRow(makeTorrent({ hash: "a" }), prev.row.id) }
    expect(torrentTableRowPropsAreEqual(prev, next)).toBe(false)
  })

  it("re-renders when the row id changes", () => {
    const prev = makeProps()
    const next = { ...prev, row: makeRow(prev.row.original, "other-id") }
    expect(torrentTableRowPropsAreEqual(prev, next)).toBe(false)
  })

  it("re-renders when a table-level display token changes identity", () => {
    const prev = makeProps()
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, columnSizing: {} })).toBe(false)
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, columns: [] })).toBe(false)
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, menu: {} as TorrentTableRowProps["menu"] })).toBe(false)
  })

  it("re-renders on primitive prop changes", () => {
    const prev = makeProps()
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, isRowSelected: true })).toBe(false)
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, virtualStart: 40 })).toBe(false)
    expect(torrentTableRowPropsAreEqual(prev, { ...prev, desktopViewMode: "dense" })).toBe(false)
  })

  it("compares props it does not know about, so a new prop is never silently ignored", () => {
    const prev = makeProps()
    const next = { ...prev, futureProp: 1 } as TorrentTableRowProps
    const prevWithOld = { ...prev, futureProp: 0 } as TorrentTableRowProps
    expect(torrentTableRowPropsAreEqual(prevWithOld, next)).toBe(false)
  })
})
