/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentRow } from "@/components/torrents/tanstackTableFeatures"
import type { Torrent } from "@/types"
import type { Virtualizer } from "@tanstack/react-virtual"
import { useEffect } from "react"

export interface UseRevealRowParams {
  target: Torrent | null | undefined
  rows: TorrentRow[]
  totalCount: number
  safeLoadedRows: number
  isLoading: boolean
  loadMore: () => void
  virtualizer: Pick<Virtualizer<HTMLDivElement, Element>, "scrollToIndex">
  getSelectionIdentity: (torrent: Torrent) => string
  onDone?: () => void
}

export function useRevealRow({ target, rows, totalCount, safeLoadedRows, isLoading, loadMore, virtualizer, getSelectionIdentity, onDone }: UseRevealRowParams) {
  useEffect(() => {
    if (!target || isLoading) return
    if (rows.length === 0 && totalCount > 0) return
    const key = getSelectionIdentity(target)
    const index = rows.findIndex(row => getSelectionIdentity(row.original) === key)
    if (index >= safeLoadedRows) {
      loadMore()
      return
    }
    if (index !== -1) virtualizer.scrollToIndex(index, { align: "center" })
    onDone?.()
  }, [getSelectionIdentity, isLoading, loadMore, onDone, rows, safeLoadedRows, target, totalCount, virtualizer])
}
