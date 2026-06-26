/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableHotkeys } from "@/hooks/torrent-table/useTorrentTableHotkeys"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { createRef } from "react"
import { describe, expect, it, vi } from "vitest"

function render(sortedTorrents: Torrent[]) {
  const setIsAllSelected = vi.fn()
  const setExcludedFromSelectAll = vi.fn()
  const setRowSelection = vi.fn()
  const lastSelectedIndexRef = createRef<number | null>() as { current: number | null }
  lastSelectedIndexRef.current = 5
  const hook = renderHook(() =>
    useTorrentTableHotkeys({
      sortedTorrents,
      setIsAllSelected,
      setExcludedFromSelectAll,
      setRowSelection,
      lastSelectedIndexRef,
    })
  )
  return { hook, setIsAllSelected, setExcludedFromSelectAll, setRowSelection, lastSelectedIndexRef }
}

describe("useTorrentTableHotkeys", () => {
  it("exposes a boolean isMac platform flag", () => {
    const { hook } = render([])
    expect(typeof hook.result.current.isMac).toBe("boolean")
  })

  it("selects all and resets the range anchor when there are torrents", () => {
    const ctx = render([makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })])
    act(() => ctx.hook.result.current.selectAllWithShortcut())
    expect(ctx.setIsAllSelected).toHaveBeenCalledWith(true)
    expect(ctx.setExcludedFromSelectAll).toHaveBeenCalled()
    expect(ctx.setRowSelection).toHaveBeenCalledWith({})
    expect(ctx.lastSelectedIndexRef.current).toBeNull()
  })

  it("is a no-op when there are no torrents", () => {
    const ctx = render([])
    act(() => ctx.hook.result.current.selectAllWithShortcut())
    expect(ctx.setIsAllSelected).not.toHaveBeenCalled()
    expect(ctx.setRowSelection).not.toHaveBeenCalled()
  })
})
