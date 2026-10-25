/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  useTorrentTableArrowNavigation,
  type UseTorrentTableArrowNavigationParams
} from "@/hooks/torrent-table/useTorrentTableArrowNavigation"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { Torrent } from "@/types"
import type { Virtualizer } from "@tanstack/react-virtual"
import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const scrollToIndex = vi.fn()
const virtualizer = { scrollToIndex } as unknown as Virtualizer<HTMLDivElement, Element>

function makeRows(n: number): TorrentRow[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `hash${i}`,
    original: { hash: `hash${i}`, name: `torrent ${i}` } as Torrent,
  })) as unknown as TorrentRow[]
}

function baseProps(over: Partial<UseTorrentTableArrowNavigationParams> = {}): UseTorrentTableArrowNavigationParams {
  return {
    rows: makeRows(10),
    virtualizer,
    safeLoadedRows: 10,
    loadMore: vi.fn(),
    isReadOnly: false,
    selectedTorrent: null,
    selectedKeys: new Set(),
    lastSelectedIndexRef: { current: null },
    getSelectionIdentity: (torrent: Torrent) => torrent.hash,
    selectOnly: vi.fn(),
    onTorrentSelect: vi.fn(),
    ...over,
  }
}

function press(key: string, init: KeyboardEventInit = {}) {
  const event = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...init })
  act(() => {
    window.dispatchEvent(event)
  })
  return event
}

beforeEach(() => {
  scrollToIndex.mockClear()
})

// No global setup file, so RTL's auto-cleanup never runs: without this each
// hook's window listener survives and answers the next test's keypress.
afterEach(cleanup)

describe("useTorrentTableArrowNavigation", () => {
  it("starts at the first row when nothing is focused", () => {
    const props = baseProps()
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(props.selectOnly).toHaveBeenCalledWith(props.rows[0].original, 0)
    expect(scrollToIndex).toHaveBeenCalledWith(0)
  })

  it("moves relative to the focused torrent, not a stored index", () => {
    // Rows re-sorted since the click: the anchor index is stale, identity is not.
    const props = baseProps({
      rows: makeRows(10),
      selectedTorrent: { hash: "hash4" } as Torrent,
      lastSelectedIndexRef: { current: 9 },
    })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(props.selectOnly).toHaveBeenCalledWith(props.rows[5].original, 5)
    expect(props.onTorrentSelect).toHaveBeenCalledWith(props.rows[5].original)
  })

  it("clamps at both ends", () => {
    const props = baseProps({ selectedKeys: new Set(["hash0"]) })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowUp")

    expect(props.selectOnly).toHaveBeenCalledWith(props.rows[0].original, 0)
    expect(scrollToIndex).toHaveBeenCalledWith(0)
  })

  it("does not open the details panel while it is closed", () => {
    const props = baseProps({ selectedKeys: new Set(["hash2"]) })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(props.selectOnly).toHaveBeenCalledWith(props.rows[3].original, 3)
    expect(props.onTorrentSelect).not.toHaveBeenCalled()
  })

  it("opens the details panel on Enter", () => {
    const props = baseProps({ selectedKeys: new Set(["hash2"]) })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("Enter")

    expect(props.onTorrentSelect).toHaveBeenCalledWith(props.rows[2].original)
  })

  it("ignores Enter when the panel is already open", () => {
    const props = baseProps({ selectedTorrent: { hash: "hash2" } as Torrent })
    renderHook(() => useTorrentTableArrowNavigation(props))

    const event = press("Enter")

    expect(props.onTorrentSelect).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(false)
  })

  it("drives only the panel in read-only mode", () => {
    const props = baseProps({ isReadOnly: true, selectedTorrent: { hash: "hash1" } as Torrent })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(props.selectOnly).not.toHaveBeenCalled()
    expect(props.onTorrentSelect).toHaveBeenCalledWith(props.rows[2].original)
  })

  it("pulls more rows when nearing the loaded window", () => {
    const props = baseProps({ rows: makeRows(200), safeLoadedRows: 100, selectedKeys: new Set(["hash59"]) })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(scrollToIndex).toHaveBeenCalledWith(60)
    expect(props.loadMore).toHaveBeenCalled()
  })

  it("stops at the virtualized window, not the full row count", () => {
    // Rows past safeLoadedRows exist in the row model but have no virtual item,
    // so scrolling to them would land on nothing.
    const props = baseProps({ rows: makeRows(200), safeLoadedRows: 100, selectedKeys: new Set(["hash99"]) })
    renderHook(() => useTorrentTableArrowNavigation(props))

    press("ArrowDown")

    expect(scrollToIndex).toHaveBeenCalledWith(99)
    expect(props.selectOnly).toHaveBeenCalledWith(props.rows[99].original, 99)
  })

  it("stays out of the way of typing, modifiers and handled events", () => {
    const props = baseProps()
    renderHook(() => useTorrentTableArrowNavigation(props))

    const input = document.createElement("input")
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }))
    input.remove()

    // Arrows inside the details panel scroll its own content.
    const panel = document.createElement("div")
    panel.setAttribute("data-torrent-details-panel", "")
    const inPanel = document.createElement("button")
    panel.appendChild(inPanel)
    document.body.appendChild(panel)
    inPanel.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }))
    panel.remove()

    press("ArrowDown", { shiftKey: true })
    press("ArrowDown", { ctrlKey: true })

    expect(props.selectOnly).not.toHaveBeenCalled()
    expect(scrollToIndex).not.toHaveBeenCalled()
  })
})
