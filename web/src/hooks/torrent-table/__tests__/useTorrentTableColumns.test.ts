/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type UseTorrentTableColumnsParams, useTorrentTableColumns } from "@/hooks/torrent-table/useTorrentTableColumns"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { renderHook } from "@testing-library/react"
import type { TFunction } from "i18next"
import type { RefObject } from "react"
import { describe, expect, it, vi } from "vitest"

const passthroughT = ((key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? key) as unknown as TFunction

function makeParams(overrides: Partial<UseTorrentTableColumnsParams> = {}): UseTorrentTableColumnsParams {
  return {
    shiftPressedRef: { current: false } as RefObject<boolean>,
    lastSelectedIndexRef: { current: null } as RefObject<number | null>,
    handleSelectAll: vi.fn(),
    isSelectAllChecked: false,
    isSelectAllIndeterminate: false,
    handleRowSelection: vi.fn(),
    getSelectionIdentity: (t: Torrent) => t.hash,
    isAllSelected: false,
    excludedFromSelectAll: new Set(),
    incognitoMode: false,
    speedUnit: "bytes",
    trackerIcons: undefined,
    formatTimestamp: (n: number) => String(n),
    preferences: null,
    supportsTrackerHealth: false,
    isUnifiedView: false,
    isCrossInstanceEndpoint: false,
    desktopViewMode: "normal",
    trackerCustomizationLookup: undefined,
    isReadOnly: false,
    t: passthroughT,
    sortedTorrents: [],
    ...overrides,
  }
}

describe("useTorrentTableColumns", () => {
  it("returns a non-empty column set and per-identity counts", () => {
    const params = makeParams({
      sortedTorrents: [
        makeTorrent({ hash: "dup" }),
        makeTorrent({ hash: "dup" }),
        makeTorrent({ hash: "uniq" }),
      ],
    })
    const { result } = renderHook(() => useTorrentTableColumns(params))
    expect(result.current.columns.length).toBeGreaterThan(0)
    expect(result.current.torrentIdentityCounts.get("dup")).toBe(2)
    expect(result.current.torrentIdentityCounts.get("uniq")).toBe(1)
  })

  it("skips torrents with no usable identity", () => {
    const params = makeParams({
      sortedTorrents: [makeTorrent({ hash: "", infohash_v1: "", infohash_v2: "" })],
    })
    const { result } = renderHook(() => useTorrentTableColumns(params))
    expect(result.current.torrentIdentityCounts.size).toBe(0)
  })

  it("keeps a stable columns reference across renders with identical inputs", () => {
    const params = makeParams()
    const { result, rerender } = renderHook(() => useTorrentTableColumns(params))
    const first = result.current.columns
    rerender()
    expect(result.current.columns).toBe(first)
  })

  it("includes the selection column only when not read-only", () => {
    const editable = renderHook(() => useTorrentTableColumns(makeParams({ isReadOnly: false })))
    const readOnly = renderHook(() => useTorrentTableColumns(makeParams({ isReadOnly: true })))
    expect(editable.result.current.columns.length).toBeGreaterThan(readOnly.result.current.columns.length)
  })
})
