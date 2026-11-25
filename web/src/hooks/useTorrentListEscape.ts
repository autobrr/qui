/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"

export interface UseTorrentListEscapeParams {
  hasSelection: boolean
  hasSearch: boolean
  clearSelection: () => void
  clearSearch: () => void
}

const TEXT_FIELD = "input, textarea, select, [contenteditable='true']"

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
