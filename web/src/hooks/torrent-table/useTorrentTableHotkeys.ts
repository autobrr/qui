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

export function useTorrentTableHotkeys({
  sortedTorrents,
  selectAll,
}: UseTorrentTableHotkeysParams) {
  const isMac = useMemo(() => {
    return typeof window !== "undefined" && /Mac|iPhone|iPad|iPod/.test(window.navigator.userAgent)
  }, [])

  const selectAllWithShortcut = useCallback(() => {
    if (sortedTorrents.length === 0) {
      return
    }

    selectAll()
  }, [sortedTorrents.length, selectAll])

  return { isMac, selectAllWithShortcut }
}
