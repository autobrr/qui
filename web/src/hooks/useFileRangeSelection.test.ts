/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useFileRangeSelection } from "@/hooks/useFileRangeSelection"
import type { RangeRow } from "@/lib/file-selection"
import type { TorrentFile } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

const fileRow = (index: number): RangeRow => ({ node: { file: { index } } })
const folderRow = (): RangeRow => ({ node: {} })
const asFile = (index: number) => ({ index } as TorrentFile)
const pointer = (shiftKey: boolean) => ({ shiftKey } as React.PointerEvent<HTMLButtonElement>)

function setup(rows: RangeRow[], resetKey = "hashA") {
  const onToggleFile = vi.fn()
  const onToggleFileRange = vi.fn()
  const view = renderHook(
    ({ rk }) => useFileRangeSelection({ getRows: () => rows, onToggleFile, onToggleFileRange, resetKey: rk }),
    { initialProps: { rk: resetKey } }
  )
  return { ...view, onToggleFile, onToggleFileRange }
}

describe("useFileRangeSelection", () => {
  it("plain click toggles a single file and sets the anchor", () => {
    const { result, onToggleFile, onToggleFileRange } = setup([fileRow(10), fileRow(11), fileRow(12)])
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(false))
      result.current.handleFileCheckbox(asFile(10), 0, true)
    })
    expect(onToggleFile).toHaveBeenCalledWith(expect.objectContaining({ index: 10 }), true)
    expect(onToggleFileRange).not.toHaveBeenCalled()
  })

  it("shift+click after an anchor toggles the inclusive range, not a single file", () => {
    const { result, onToggleFile, onToggleFileRange } = setup([fileRow(10), fileRow(11), fileRow(12), fileRow(13)])
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(false))
      result.current.handleFileCheckbox(asFile(10), 0, true)
    })
    onToggleFile.mockClear()
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(true))
      result.current.handleFileCheckbox(asFile(13), 3, true)
    })
    expect(onToggleFileRange).toHaveBeenCalledWith([10, 11, 12, 13], true)
    expect(onToggleFile).not.toHaveBeenCalled()
  })

  it("excludes folder rows from the range", () => {
    const { result, onToggleFileRange } = setup([fileRow(10), folderRow(), fileRow(11), fileRow(12)])
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(false))
      result.current.handleFileCheckbox(asFile(10), 0, true)
    })
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(true))
      result.current.handleFileCheckbox(asFile(12), 3, true)
    })
    expect(onToggleFileRange).toHaveBeenCalledWith([10, 11, 12], true)
  })

  it("shift+click with no prior anchor falls back to a single toggle", () => {
    const { result, onToggleFile, onToggleFileRange } = setup([fileRow(10), fileRow(11)])
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(true))
      result.current.handleFileCheckbox(asFile(11), 1, true)
    })
    expect(onToggleFile).toHaveBeenCalledWith(expect.objectContaining({ index: 11 }), true)
    expect(onToggleFileRange).not.toHaveBeenCalled()
  })

  it("falls back to a single toggle when the anchor is no longer in the rows", () => {
    let rows: RangeRow[] = [fileRow(10), fileRow(11), fileRow(12)]
    const onToggleFile = vi.fn()
    const onToggleFileRange = vi.fn()
    const { result, rerender } = renderHook(
      ({ rk }) => useFileRangeSelection({ getRows: () => rows, onToggleFile, onToggleFileRange, resetKey: rk }),
      { initialProps: { rk: "hashA" } }
    )
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(false))
      result.current.handleFileCheckbox(asFile(10), 0, true)
    })
    // Anchor (index 10) gets filtered out of the visible rows (e.g. a search).
    rows = [fileRow(11), fileRow(12)]
    rerender({ rk: "hashA" })
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(true))
      result.current.handleFileCheckbox(asFile(12), 1, true)
    })
    expect(onToggleFileRange).not.toHaveBeenCalled()
    expect(onToggleFile).toHaveBeenLastCalledWith(expect.objectContaining({ index: 12 }), true)
  })

  it("drops the anchor when resetKey changes (torrent switch)", () => {
    const rows: RangeRow[] = [fileRow(10), fileRow(11), fileRow(12)]
    const onToggleFile = vi.fn()
    const onToggleFileRange = vi.fn()
    const { result, rerender } = renderHook(
      ({ rk }) => useFileRangeSelection({ getRows: () => rows, onToggleFile, onToggleFileRange, resetKey: rk }),
      { initialProps: { rk: "hashA" } }
    )
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(false))
      result.current.handleFileCheckbox(asFile(10), 0, true)
    })
    rerender({ rk: "hashB" }) // user switched to a different torrent
    act(() => {
      result.current.handleCheckboxPointerDown(pointer(true))
      result.current.handleFileCheckbox(asFile(12), 2, true)
    })
    expect(onToggleFileRange).not.toHaveBeenCalled()
    expect(onToggleFile).toHaveBeenLastCalledWith(expect.objectContaining({ index: 12 }), true)
  })
})
