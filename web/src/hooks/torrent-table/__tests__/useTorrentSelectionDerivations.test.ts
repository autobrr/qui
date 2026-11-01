/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SelectionRow } from "@/hooks/torrent-table/useTorrentSelection"
import { type UseTorrentSelectionDerivationsParams, useTorrentSelectionDerivations } from "@/hooks/torrent-table/useTorrentSelectionDerivations"
import { EMPTY_SELECTION, type TorrentSelectionState } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import "@/i18n"

// The Action target rules live in the table test of buildActionTarget. These
// tests cover what the desktop table adds: its row model, the remembered ticks,
// and the sizes.

const TORRENTS = [
  makeTorrent({ hash: "h0", name: "t0", size: 100 }),
  makeTorrent({ hash: "h1", name: "t1", size: 200 }),
  makeTorrent({ hash: "h2", name: "t2", size: 300 }),
]

function makeRows(torrents: Torrent[]): SelectionRow[] {
  return torrents.map((original, index) => ({ id: String(index), original }))
}

function ticked(...keys: string[]): TorrentSelectionState {
  return { ...EMPTY_SELECTION, selected: new Set(keys) }
}

function allSelected(...excluded: string[]): TorrentSelectionState {
  return { ...EMPTY_SELECTION, isAllSelected: true, excluded: new Set(excluded) }
}

function render(overrides: Partial<UseTorrentSelectionDerivationsParams> = {}) {
  const params: UseTorrentSelectionDerivationsParams = {
    selection: EMPTY_SELECTION,
    getSelectionIdentity: (t: Torrent) => t.hash,
    getVisibleRows: () => makeRows(TORRENTS),
    sortedTorrents: TORRENTS,
    columnFiltersExpr: null,
    filters: makeFilters(),
    search: "",
    stats: { totalSize: 600 },
    totalCount: 3,
    isCrossInstanceEndpoint: false,
    instanceId: 1,
    ...overrides,
  }
  return {
    ...renderHook((p: UseTorrentSelectionDerivationsParams) => useTorrentSelectionDerivations(p), { initialProps: params }),
    params,
  }
}

const previewHashes = (result: { current: ReturnType<typeof useTorrentSelectionDerivations> }) =>
  result.current.actionTarget.previewTorrents.map(t => t.hash)

describe("useTorrentSelectionDerivations — the rows of the Action target", () => {
  // A sort keeps the ticks, so a ticked torrent can move past the loaded rows.
  it("keeps a ticked torrent that left the visible rows", () => {
    const { result, rerender, params } = render({ selection: ticked("h0", "h2") })
    rerender({ ...params, getVisibleRows: () => makeRows([TORRENTS[1]]), sortedTorrents: [TORRENTS[1]] })
    expect(previewHashes(result)).toEqual(["h0", "h2"])
    expect(result.current.actionTarget.count).toBe(2)
  })

  // #1925 on the client side: in cross-seed mode the column filter hides rows
  // client-side, so select-all must resolve to the rows the user sees, not to
  // every loaded row. Copy-all reads this list when everything is loaded.
  it("resolves the visible rows in select-all mode when the column filter applies client-side", () => {
    const { result, rerender, params } = render({
      selection: allSelected(),
      columnFiltersExpr: "state == \"downloading\"",
      clientSideFiltering: true,
      getVisibleRows: () => makeRows([TORRENTS[1]]),
      totalCount: 3,
    })
    expect(previewHashes(result)).toEqual(["h1"])
    // The count follows the visible rows, not the backend total (the menu
    // label "Pause (6)" must not promise 6 when the action reaches 2).
    expect(result.current.actionTarget.count).toBe(1)
    expect(result.current.selectedTotalSize).toBe(TORRENTS[1].size)

    // Clearing the column filter widens the visible set again.
    rerender({ ...params, columnFiltersExpr: null, getVisibleRows: () => makeRows(TORRENTS) })
    expect(previewHashes(result)).toEqual(["h0", "h1", "h2"])
  })
})

describe("useTorrentSelectionDerivations — selectedTotalSize", () => {
  it("sums the selected torrents in regular mode", () => {
    const { result } = render({ selection: ticked("h0", "h1") })
    expect(result.current.selectedTotalSize).toBe(300)
  })

  it("uses the aggregate stats total in select-all mode with no exclusions", () => {
    const { result } = render({ selection: allSelected(), stats: { totalSize: 600 } })
    expect(result.current.selectedTotalSize).toBe(600)
  })

  it("subtracts excluded sizes from the aggregate in select-all mode", () => {
    const { result } = render({ selection: allSelected("h1"), stats: { totalSize: 600 } })
    expect(result.current.selectedTotalSize).toBe(400)
  })
})

describe("useTorrentSelectionDerivations — no-selection identity across stream ticks", () => {
  // Each stream tick hands the hook a fresh sortedTorrents array. With nothing
  // selected the Action target must keep its identity, or every memo
  // downstream (row menus, the selection context) re-runs once per tick.
  it("returns the same Action target when sortedTorrents is replaced", () => {
    const { result, rerender, params } = render({ sortedTorrents: [...TORRENTS] })
    const first = result.current

    rerender({ ...params, sortedTorrents: [...TORRENTS] })

    expect(result.current.actionTarget).toBe(first.actionTarget)
  })
})

describe("useTorrentSelectionDerivations — deleteDialogTotalSize", () => {
  it("prefers the aggregate selected size in select-all mode", () => {
    const { result } = render({ selection: allSelected(), stats: { totalSize: 600 } })
    expect(result.current.deleteDialogTotalSize).toBe(600)
  })

  it("falls back to the loaded torrents in select-all mode without an aggregate size", () => {
    const { result } = render({ selection: allSelected("h0"), stats: undefined })
    expect(result.current.deleteDialogTotalSize).toBe(500)
  })
})
