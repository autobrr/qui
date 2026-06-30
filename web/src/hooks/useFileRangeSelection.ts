/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { collectFileIndicesInRange, type RangeRow } from "@/lib/file-selection"
import type { TorrentFile } from "@/types"
import { useCallback, useRef } from "react"

export interface UseFileRangeSelectionParams {
  /**
   * Lazily returns the current ordered, visible row list. A callback because the
   * rows (filteredRows / flatRows) are derived after this hook runs, so the
   * latest reference is read at click time rather than captured at call time.
   */
  getRows: () => ReadonlyArray<RangeRow>
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFileRange: (indices: number[], selected: boolean) => void
  /**
   * Identity of the current list (e.g. the torrent hash). When it changes the
   * range anchor is dropped: TorrentFileTable is reused across torrent switches
   * and file indices are per-torrent, so a stale anchor would mis-target the
   * next torrent's rows. Harmless for the keyed TorrentFileTree, which remounts.
   */
  resetKey: string
}

export interface FileRangeSelection {
  handleCheckboxPointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void
  clearShift: () => void
  handleFileCheckbox: (file: TorrentFile, rowPos: number, checked: boolean) => void
}

/**
 * Shift+click range selection for the Content tab's file checkboxes. The shift
 * modifier must be captured on pointer down because Radix Checkbox's
 * onCheckedChange carries no event. Mirrors the torrent table's range pattern.
 */
export function useFileRangeSelection({
  getRows,
  onToggleFile,
  onToggleFileRange,
  resetKey,
}: UseFileRangeSelectionParams): FileRangeSelection {
  const shiftPressedRef = useRef(false)
  const lastClickedFileIndexRef = useRef<number | null>(null)

  // Always read the latest row accessor (rows are derived after this hook runs).
  const getRowsRef = useRef(getRows)
  getRowsRef.current = getRows

  // Drop the anchor when the list identity changes (render-time reset).
  const resetKeyRef = useRef(resetKey)
  if (resetKeyRef.current !== resetKey) {
    resetKeyRef.current = resetKey
    lastClickedFileIndexRef.current = null
  }

  const handleCheckboxPointerDown = useCallback((event: React.PointerEvent<HTMLButtonElement>) => {
    shiftPressedRef.current = event.shiftKey
  }, [])

  const clearShift = useCallback(() => {
    shiftPressedRef.current = false
  }, [])

  // Toggle a file checkbox, extending to a contiguous range when shift is held.
  // rowPos is the clicked file's position within the ordered, visible row list.
  const handleFileCheckbox = useCallback((file: TorrentFile, rowPos: number, checked: boolean) => {
    const shift = shiftPressedRef.current
    shiftPressedRef.current = false

    if (shift && lastClickedFileIndexRef.current !== null) {
      const rows = getRowsRef.current()
      const anchorPos = rows.findIndex(row => row.node.file?.index === lastClickedFileIndexRef.current)
      if (anchorPos !== -1) {
        const indices = collectFileIndicesInRange(rows, anchorPos, rowPos)
        onToggleFileRange(indices, checked)
        lastClickedFileIndexRef.current = file.index
        return
      }
    }

    onToggleFile(file, checked)
    lastClickedFileIndexRef.current = file.index
  }, [onToggleFile, onToggleFileRange])

  return { handleCheckboxPointerDown, clearShift, handleFileCheckbox }
}
