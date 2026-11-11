/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"
import { useCallback, useMemo } from "react"

export interface UseTorrentTableHotkeysParams {
  sortedTorrents: Torrent[]
  selectAll: () => void
}

/**
 * Keyboard-shortcut wiring for the table: platform detection (for showing
 * ⌘ vs Ctrl) and the Cmd/Ctrl+A "select all" handler consumed by SelectAllHotkey.
 */
export function useTorrentTableHotkeys({
  sortedTorrents,
  selectAll,
}: UseTorrentTableHotkeysParams) {
  // Detect platform for keyboard shortcuts
  const isMac = useMemo(() => {
    return typeof window !== "undefined" && /Mac|iPhone|iPad|iPod/.test(window.navigator.userAgent)
  }, [])

  // Apply Ctrl/Cmd+A shortcut to select all torrents
  const selectAllWithShortcut = useCallback(() => {
    if (sortedTorrents.length === 0) {
      return
    }

    selectAll()
  }, [sortedTorrents.length, selectAll])

  return { isMac, selectAllWithShortcut }
}
