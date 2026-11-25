/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { collectFileIndicesInRange, type RangeRow } from "@/lib/file-selection"
import type { TorrentFile } from "@/types"
import { useCallback, useRef } from "react"

export interface UseFileRangeSelectionParams {
  getRows: () => ReadonlyArray<RangeRow>
  onToggleFile: (file: TorrentFile, selected: boolean) => void
  onToggleFileRange: (indices: number[], selected: boolean) => void
  resetKey: string
}

export interface FileRangeSelection {
  handleCheckboxPointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void
  clearShift: () => void
  handleFileCheckbox: (file: TorrentFile, rowPos: number, checked: boolean) => void
}

export function useFileRangeSelection({
  getRows,
  onToggleFile,
  onToggleFileRange,
  resetKey,
}: UseFileRangeSelectionParams): FileRangeSelection {
  const shiftPressedRef = useRef(false)
  const lastClickedFileIndexRef = useRef<number | null>(null)

  const getRowsRef = useRef(getRows)
  getRowsRef.current = getRows

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
