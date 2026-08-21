/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { RowSelectionState } from "@tanstack/react-table"
import type { Virtualizer } from "@tanstack/react-virtual"
import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react"

// Mirrors useKeyboardNavigation: start pulling more rows this far from the end.
const LOAD_MORE_THRESHOLD = 50

export interface UseTorrentTableArrowNavigationParams {
  rows: TorrentRow[]
  virtualizer: Virtualizer<HTMLDivElement, Element>
  /** Virtualized row count (progressive window), which can trail rows.length. */
  safeLoadedRows: number
  loadMore: () => void
  isReadOnly: boolean
  selectedTorrent?: Torrent | null
  selectedRowIds: string[]
  lastSelectedIndexRef: RefObject<number | null>
  getSelectionIdentity: (torrent: Torrent) => string
  setRowSelection: Dispatch<SetStateAction<RowSelectionState>>
  setIsAllSelected: Dispatch<SetStateAction<boolean>>
  setExcludedFromSelectAll: Dispatch<SetStateAction<Set<string>>>
  onTorrentSelect?: (torrent: Torrent | null) => void
}

function isTypingTarget(target: EventTarget | null): boolean {
  const element = target instanceof Element ? target : null
  if (!element) {
    return false
  }

  return (
    element.tagName === "INPUT" ||
    element.tagName === "TEXTAREA" ||
    element.tagName === "SELECT" ||
    (element instanceof HTMLElement && element.isContentEditable) ||
    element.closest("[role=\"dialog\"]") !== null ||
    element.closest("[role=\"combobox\"]") !== null ||
    // Focus inside the details panel: arrows belong to its own content there.
    element.closest("[data-torrent-details-panel]") !== null
  )
}

/**
 * Arrow up/down moves the focused row the same way a plain click does (replace
 * selection with that single row), and Enter opens the details panel for it.
 * The panel only *follows* the arrows once it is already open, so navigating a
 * list never pops it up; Escape (handled in Torrents.tsx) closes it again.
 */
export function useTorrentTableArrowNavigation({
  rows,
  virtualizer,
  safeLoadedRows,
  loadMore,
  isReadOnly,
  selectedTorrent,
  selectedRowIds,
  lastSelectedIndexRef,
  getSelectionIdentity,
  setRowSelection,
  setIsAllSelected,
  setExcludedFromSelectAll,
  onTorrentSelect,
}: UseTorrentTableArrowNavigationParams) {
  useEffect(() => {
    const findCursorIndex = (): number => {
      // Resolve by identity rather than a stored index: rows re-sort live, so a
      // remembered index drifts onto a different torrent.
      if (selectedTorrent) {
        const identity = getSelectionIdentity(selectedTorrent)
        const index = rows.findIndex(row => getSelectionIdentity(row.original) === identity)
        if (index !== -1) {
          return index
        }
      }

      if (selectedRowIds.length === 1) {
        const index = rows.findIndex(row => row.id === selectedRowIds[0])
        if (index !== -1) {
          return index
        }
      }

      // Multi-selection: fall back to the click anchor.
      const anchor = lastSelectedIndexRef.current
      return anchor !== null && anchor >= 0 && anchor < rows.length ? anchor : -1
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) {
        return
      }

      if (event.key !== "ArrowUp" && event.key !== "ArrowDown" && event.key !== "Enter") {
        return
      }

      if (isTypingTarget(event.target)) {
        return
      }

      if (rows.length === 0 || safeLoadedRows === 0) {
        return
      }

      const cursorIndex = findCursorIndex()

      if (event.key === "Enter") {
        const row = rows[cursorIndex]
        if (!row || selectedTorrent) {
          return
        }

        event.preventDefault()
        onTorrentSelect?.(row.original)
        return
      }

      const delta = event.key === "ArrowDown" ? 1 : -1
      const lastIndex = safeLoadedRows - 1
      const targetIndex = cursorIndex === -1? (delta > 0 ? 0 : lastIndex): Math.min(lastIndex, Math.max(0, cursorIndex + delta))

      const targetRow = rows[targetIndex]
      if (!targetRow) {
        return
      }

      event.preventDefault()

      if (isReadOnly) {
        // Read-only mode has no row selection, so the panel is the only cursor.
        onTorrentSelect?.(targetRow.original)
      } else {
        setIsAllSelected(false)
        setExcludedFromSelectAll(new Set())
        setRowSelection({ [targetRow.id]: true })
        lastSelectedIndexRef.current = targetIndex

        if (selectedTorrent) {
          onTorrentSelect?.(targetRow.original)
        }
      }

      virtualizer.scrollToIndex(targetIndex)

      if (targetIndex >= safeLoadedRows - LOAD_MORE_THRESHOLD) {
        loadMore()
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    return () => {
      window.removeEventListener("keydown", handleKeyDown)
    }
  }, [
    rows,
    virtualizer,
    safeLoadedRows,
    loadMore,
    isReadOnly,
    selectedTorrent,
    selectedRowIds,
    getSelectionIdentity,
    onTorrentSelect,
    lastSelectedIndexRef,
    setRowSelection,
    setIsAllSelected,
    setExcludedFromSelectAll,
  ])
}
