/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"

export interface UseTorrentListEscapeParams {
  /** The details panel is open, or rows are selected. */
  hasSelection: boolean
  /** The list has a search text. */
  hasSearch: boolean
  clearSelection: () => void
  clearSearch: () => void
}

const TEXT_FIELD = "input, textarea, select, [contenteditable='true']"

/**
 * Escape on a torrent list. Each press clears one level: first the details panel
 * and the selected rows, then the search text of the list. An open dialog keeps
 * its own Escape. A text field outside the header search, which handles its own
 * Escape, does not clear the search of the list.
 */
export function useTorrentListEscape({ hasSelection, hasSearch, clearSelection, clearSearch }: UseTorrentListEscapeParams) {
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return
      if (document.querySelector("[role=\"dialog\"]")) return

      e.preventDefault()
      if (hasSelection) {
        clearSelection()
      } else if (hasSearch && !(e.target instanceof Element && e.target.closest(TEXT_FIELD))) {
        clearSearch()
      }
    }

    window.addEventListener("keydown", handleEscape)
    return () => window.removeEventListener("keydown", handleEscape)
  }, [clearSearch, clearSelection, hasSearch, hasSelection])
}
