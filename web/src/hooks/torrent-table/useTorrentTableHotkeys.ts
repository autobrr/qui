/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"
import { type Dispatch, type RefObject, type SetStateAction, useCallback, useMemo } from "react"

export interface UseTorrentTableHotkeysParams {
  sortedTorrents: Torrent[]
  setIsAllSelected: Dispatch<SetStateAction<boolean>>
  setExcludedFromSelectAll: Dispatch<SetStateAction<Set<string>>>
  setRowSelection: Dispatch<SetStateAction<Record<string, boolean>>>
  lastSelectedIndexRef: RefObject<number | null>
}

/**
 * Keyboard-shortcut wiring for the table: platform detection (for showing
 * ⌘ vs Ctrl) and the Cmd/Ctrl+A "select all" handler consumed by SelectAllHotkey.
 */
export function useTorrentTableHotkeys({
  sortedTorrents,
  setIsAllSelected,
  setExcludedFromSelectAll,
  setRowSelection,
  lastSelectedIndexRef,
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

    setIsAllSelected(true)
    setExcludedFromSelectAll(new Set())
    setRowSelection({})
    lastSelectedIndexRef.current = null
  }, [sortedTorrents.length, setIsAllSelected, setExcludedFromSelectAll, setRowSelection, lastSelectedIndexRef])

  return { isMac, selectAllWithShortcut }
}
