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
  /** The number of rows that the server reports for the list. */
  totalCount: number
  /** The rows that the table renders. The table cannot scroll to a row past them. */
  safeLoadedRows: number
  isLoading: boolean
  /** Renders more of the loaded rows. */
  loadMore: () => void
  virtualizer: Pick<Virtualizer<HTMLDivElement, Element>, "scrollToIndex">
  getSelectionIdentity: (torrent: Torrent) => string
  onDone?: () => void
}

/**
 * Scrolls the table to the row of `target`, then calls `onDone`. While the list loads,
 * the request waits. A list that loaded without the row also calls `onDone`, with no
 * scroll, so a later page or live update does not move the table.
 */
export function useRevealRow({ target, rows, totalCount, safeLoadedRows, isLoading, loadMore, virtualizer, getSelectionIdentity, onDone }: UseRevealRowParams) {
  useEffect(() => {
    if (!target || isLoading) return
    // The list hook sets the rows of a response one render after it arrives.
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
