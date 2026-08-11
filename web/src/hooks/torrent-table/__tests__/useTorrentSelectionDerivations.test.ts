/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SelectionRow } from "@/hooks/torrent-table/useTorrentSelection"
import { type UseTorrentSelectionDerivationsParams, useTorrentSelectionDerivations } from "@/hooks/torrent-table/useTorrentSelectionDerivations"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"

const TORRENTS = [
  makeTorrent({ hash: "h0", name: "t0", size: 100 }),
  makeTorrent({ hash: "h1", name: "t1", size: 200 }),
  makeTorrent({ hash: "h2", name: "t2", size: 300 }),
]

function makeRows(torrents: Torrent[]): SelectionRow[] {
  return torrents.map((original, index) => ({ id: String(index), original }))
}

function render(overrides: Partial<UseTorrentSelectionDerivationsParams> = {}) {
  const params: UseTorrentSelectionDerivationsParams = {
    isAllSelected: false,
    excludedFromSelectAll: new Set(),
    selectedRowIds: [],
    selectedRowIdSet: new Set(),
    getSelectionIdentity: (t: Torrent) => t.hash,
    getVisibleRows: () => makeRows(TORRENTS),
    sortedTorrents: TORRENTS,
    columnFiltersExpr: null,
    filters: undefined,
    stats: { totalSize: 600 },
    totalCount: 3,
    isCrossInstanceEndpoint: false,
    instanceId: 1,
    contextTorrents: [],
    ...overrides,
  }
  return renderHook((p: UseTorrentSelectionDerivationsParams) => useTorrentSelectionDerivations(p), { initialProps: params })
}

describe("useTorrentSelectionDerivations — selectAllFilters (#1925 pin)", () => {
  it("ALWAYS combines columnFiltersExpr with filters.expr — even in cross-seed mode", () => {
    // Regression #1925: selectAllFilters must NOT adopt combinedFiltersExpr's
    // cross-seed early-return. A column filter + a cross-seed (Hash ==/||) expr
    // must be joined with && so bulk select-all targets exactly the visible set.
    const { result } = render({
      isAllSelected: true,
      columnFiltersExpr: "state == \"downloading\"",
      filters: makeFilters({ expr: "Hash == \"abc\" || Hash == \"def\"" }),
    })
    expect(result.current.selectAllFilters?.expr).toBe(
      "(state == \"downloading\") && (Hash == \"abc\" || Hash == \"def\")"
    )
  })

  it("is undefined when not in select-all mode", () => {
    const { result } = render({ isAllSelected: false, columnFiltersExpr: "state == \"x\"" })
    expect(result.current.selectAllFilters).toBeUndefined()
  })

  it("uses filters.expr alone when there is no column filter", () => {
    const { result } = render({
      isAllSelected: true,
      filters: makeFilters({ expr: "state == \"seeding\"" }),
    })
    expect(result.current.selectAllFilters?.expr).toBe("state == \"seeding\"")
  })

  it("uses the column filter alone when there is no filters.expr", () => {
    const { result } = render({
      isAllSelected: true,
      columnFiltersExpr: "state == \"downloading\"",
      filters: makeFilters({}),
    })
    expect(result.current.selectAllFilters?.expr).toBe("state == \"downloading\"")
  })
})

describe("useTorrentSelectionDerivations — selectedHashes / selectedTorrents", () => {
  it("resolves the selected rows in regular mode", () => {
    const { result } = render({ selectedRowIdSet: new Set(["0", "2"]) })
    expect(result.current.selectedHashes).toEqual(["h0", "h2"])
    expect(result.current.selectedTorrents.map(t => t.hash)).toEqual(["h0", "h2"])
  })

  it("resolves all-minus-exclusions in select-all mode", () => {
    const { result } = render({ isAllSelected: true, excludedFromSelectAll: new Set(["h1"]) })
    expect(result.current.selectedHashes).toEqual(["h0", "h2"])
  })
})

describe("useTorrentSelectionDerivations — effectiveSelectionCount", () => {
  it("is the visible selection length in regular mode", () => {
    const { result } = render({ selectedRowIds: ["0", "1"] })
    expect(result.current.effectiveSelectionCount).toBe(2)
  })

  it("is totalCount minus exclusions in select-all mode", () => {
    const { result } = render({ isAllSelected: true, totalCount: 100, excludedFromSelectAll: new Set(["h0", "h1"]) })
    expect(result.current.effectiveSelectionCount).toBe(98)
  })
})

describe("useTorrentSelectionDerivations — selectedTotalSize", () => {
  it("sums the selected torrents in regular mode", () => {
    const { result } = render({ selectedRowIdSet: new Set(["0", "1"]) })
    expect(result.current.selectedTotalSize).toBe(300)
  })

  it("uses the aggregate stats total in select-all mode with no exclusions", () => {
    const { result } = render({ isAllSelected: true, stats: { totalSize: 600 } })
    expect(result.current.selectedTotalSize).toBe(600)
  })

  it("subtracts excluded sizes from the aggregate in select-all mode", () => {
    const { result } = render({ isAllSelected: true, stats: { totalSize: 600 }, excludedFromSelectAll: new Set(["h1"]) })
    expect(result.current.selectedTotalSize).toBe(400)
  })
})

describe("useTorrentSelectionDerivations — select-all excludes", () => {
  it("returns the exclusion hashes for a single-instance endpoint", () => {
    const { result } = render({ isAllSelected: true, excludedFromSelectAll: new Set(["h1"]) })
    expect(result.current.selectAllExcludeHashes).toEqual(["h1"])
  })

  it("returns undefined excludeHashes for a cross-instance endpoint", () => {
    const { result } = render({
      isAllSelected: true,
      excludedFromSelectAll: new Set(["h1"]),
      isCrossInstanceEndpoint: true,
    })
    expect(result.current.selectAllExcludeHashes).toBeUndefined()
  })

  it("builds excluded targets only in select-all mode with exclusions", () => {
    const empty = render({ isAllSelected: false, excludedFromSelectAll: new Set(["h1"]) })
    expect(empty.result.current.selectAllExcludedTargets).toEqual([])

    const { result } = render({ isAllSelected: true, excludedFromSelectAll: new Set(["h1"]) })
    expect(result.current.selectAllExcludedTargets).toHaveLength(1)
    expect(result.current.selectAllExcludedTargets[0]).toMatchObject({ hash: "h1" })
  })
})

describe("useTorrentSelectionDerivations — deleteDialogTotalSize", () => {
  it("prefers the aggregate selected size in select-all mode", () => {
    const { result } = render({ isAllSelected: true, stats: { totalSize: 600 } })
    expect(result.current.deleteDialogTotalSize).toBe(600)
  })

  it("falls back to context torrents when nothing is selected", () => {
    const { result } = render({ contextTorrents: [makeTorrent({ hash: "c0", size: 50 })] })
    expect(result.current.deleteDialogTotalSize).toBe(50)
  })
})
