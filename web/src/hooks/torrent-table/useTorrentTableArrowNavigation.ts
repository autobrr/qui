/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"
import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { Virtualizer } from "@tanstack/react-virtual"
import { useEffect, type RefObject } from "react"

const LOAD_MORE_THRESHOLD = 50

export interface UseTorrentTableArrowNavigationParams {
  rows: TorrentRow[]
  virtualizer: Virtualizer<HTMLDivElement, Element>
  safeLoadedRows: number
  loadMore: () => void
  isReadOnly: boolean
  selectedTorrent?: Torrent | null
  selectedKeys: ReadonlySet<string>
  lastSelectedIndexRef: RefObject<number | null>
  getSelectionIdentity: (torrent: Torrent) => string
  selectOnly: (torrent: Torrent, index: number) => void
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
    element.closest("[data-torrent-details-panel]") !== null
  )
}

export function useTorrentTableArrowNavigation({
  rows,
  virtualizer,
  safeLoadedRows,
  loadMore,
  isReadOnly,
  selectedTorrent,
  selectedKeys,
  lastSelectedIndexRef,
  getSelectionIdentity,
  selectOnly,
  onTorrentSelect,
}: UseTorrentTableArrowNavigationParams) {
  useEffect(() => {
    const findCursorIndex = (): number => {
      if (selectedTorrent) {
        const identity = getSelectionIdentity(selectedTorrent)
        const index = rows.findIndex(row => getSelectionIdentity(row.original) === identity)
        if (index !== -1) {
          return index
        }
      }

      if (selectedKeys.size === 1) {
        const [key] = selectedKeys
        const index = rows.findIndex(row => getSelectionIdentity(row.original) === key)
        if (index !== -1) {
          return index
        }
      }

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
        if (event.target instanceof Element && event.target.closest("button, a[href]")) {
          return
        }
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
        onTorrentSelect?.(targetRow.original)
      } else {
        selectOnly(targetRow.original, targetIndex)

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
    selectedKeys,
    getSelectionIdentity,
    onTorrentSelect,
    lastSelectedIndexRef,
    selectOnly,
  ])
}
