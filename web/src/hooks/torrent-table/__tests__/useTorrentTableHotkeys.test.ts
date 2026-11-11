/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableHotkeys } from "@/hooks/torrent-table/useTorrentTableHotkeys"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

function render(sortedTorrents: Torrent[]) {
  const selectAll = vi.fn()
  const hook = renderHook(() => useTorrentTableHotkeys({ sortedTorrents, selectAll }))
  return { hook, selectAll }
}

describe("useTorrentTableHotkeys", () => {
  it("selects all when there are torrents", () => {
    const ctx = render([makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })])
    act(() => ctx.hook.result.current.selectAllWithShortcut())
    expect(ctx.selectAll).toHaveBeenCalled()
  })

  it("is a no-op when there are no torrents", () => {
    const ctx = render([])
    act(() => ctx.hook.result.current.selectAllWithShortcut())
    expect(ctx.selectAll).not.toHaveBeenCalled()
  })
})
