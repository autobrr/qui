/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { Torrent } from "@/types"
import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useRevealRow, type UseRevealRowParams } from "../useRevealRow"

type ListState = Pick<UseRevealRowParams, "rows" | "safeLoadedRows" | "isLoading">

const torrent = (hash: string) => ({ hash, name: hash }) as Torrent
const toRows = (hashes: string[]) => hashes.map((hash, index) => ({ id: String(index), original: torrent(hash) })) as unknown as TorrentRow[]
const getSelectionIdentity = (value: Torrent) => value.hash

function setup(initial: ListState) {
  const virtualizer = { scrollToIndex: vi.fn() }
  const loadMore = vi.fn()
  const onDone = vi.fn()
  const target = torrent("picked")
  const hook = renderHook((list: ListState) =>
    useRevealRow({ ...list, target, loadMore, virtualizer, getSelectionIdentity, onDone }), { initialProps: initial })
  return { ...hook, virtualizer, loadMore, onDone }
}

afterEach(cleanup)

describe("useRevealRow", () => {
  // regression: #4260
  it("waits while the new search loads, then scrolls to the row", () => {
    const { rerender, virtualizer, onDone } = setup({ rows: toRows(["old"]), safeLoadedRows: 1, isLoading: true })
    expect(onDone).not.toHaveBeenCalled()

    rerender({ rows: toRows(["a", "b", "picked"]), safeLoadedRows: 3, isLoading: false })

    expect(virtualizer.scrollToIndex).toHaveBeenCalledExactlyOnceWith(2, { align: "center" })
    expect(onDone).toHaveBeenCalledOnce()
  })

  // regression: #4260
  it("renders more rows when the row is loaded past the rendered rows, then scrolls to it", () => {
    const rows = toRows(["a", "b", "picked"])
    const { rerender, virtualizer, loadMore } = setup({ rows, safeLoadedRows: 2, isLoading: false })
    expect(loadMore).toHaveBeenCalled()
    expect(virtualizer.scrollToIndex).not.toHaveBeenCalled()

    rerender({ rows, safeLoadedRows: 3, isLoading: false })

    expect(virtualizer.scrollToIndex).toHaveBeenCalledExactlyOnceWith(2, { align: "center" })
  })

  // regression: #4260
  it("gives up without a scroll when the loaded list does not hold the row", () => {
    const { virtualizer, onDone } = setup({ rows: toRows(["a", "b"]), safeLoadedRows: 2, isLoading: false })

    expect(virtualizer.scrollToIndex).not.toHaveBeenCalled()
    expect(onDone).toHaveBeenCalledOnce()
  })
})
