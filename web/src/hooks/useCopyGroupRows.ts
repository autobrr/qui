/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { countTickedCopies, isCopyGroupOpen, summarizeCopyGroups, type CopyGroupExpansion } from "@/lib/copy-groups"
import type { TorrentSelectionState } from "@/lib/torrent-selection"
import type { Torrent } from "@/types"
import { useCallback, useMemo } from "react"

/** What one row of the list shows about its Copy group. */
export interface CopyGroupRow<T extends Torrent> {
  /** Only on the top row of a group with two or more loaded members. */
  groupId?: string
  open: boolean
  /** The members other than the top row. */
  copies: number
  /** Only on a closed top row: a hidden copy has a problem. */
  hiddenProblem: boolean
  /** Only on a closed top row outside Select all mode: the ticked copies that it hides. */
  hiddenTicks: number
  /** Only on a closed top row whose first-sorted member is another copy. */
  sortedTorrent?: T
  /** A copy under the top row of an open group. */
  nestedCopy: boolean
}

const PLAIN_ROW = { open: false, copies: 0, hiddenProblem: false, hiddenTicks: 0, nestedCopy: false } as const
const NESTED_COPY = { ...PLAIN_ROW, nestedCopy: true } as const
const NO_TICKED_COPIES: ReadonlyMap<string, number> = new Map()

export interface UseCopyGroupRowsParams<T extends Torrent> {
  /** Every loaded row, the hidden copies included. */
  rows: T[]
  expansion: CopyGroupExpansion
  selection: TorrentSelectionState
  getSelectionIdentity: (row: T) => string
}

/**
 * Tells both Layouts what a row shows about its Copy group. A group with one
 * loaded member is a plain row. `describeRow` changes when a hidden copy
 * changes, so a memoized row must get its result as props.
 */
export function useCopyGroupRows<T extends Torrent>({ rows, expansion, selection, getSelectionIdentity }: UseCopyGroupRowsParams<T>) {
  const groups = useMemo(() => summarizeCopyGroups(rows), [rows])

  // Select all mode already reaches the hidden copies, so the pill counts no ticks there.
  const tickedCopies = useMemo(
    () => groups.size === 0 || selection.isAllSelected || selection.selected.size === 0 ? NO_TICKED_COPIES : countTickedCopies(rows, row => selection.selected.has(getSelectionIdentity(row))),
    [groups, selection, rows, getSelectionIdentity]
  )

  const describeRow = useCallback((row: T): CopyGroupRow<T> => {
    const id = row.copy_group?.id
    const group = id === undefined ? undefined : groups.get(id)
    if (id === undefined || group === undefined || group.size < 2) {
      return PLAIN_ROW
    }
    if (group.top !== row) {
      return NESTED_COPY
    }
    const open = isCopyGroupOpen(expansion, id)
    return {
      groupId: id,
      open,
      copies: group.size - 1,
      hiddenProblem: !open && group.hiddenProblem,
      hiddenTicks: open ? 0 : tickedCopies.get(id) ?? 0,
      sortedTorrent: !open && group.firstSorted !== row ? group.firstSorted : undefined,
      nestedCopy: false,
    }
  }, [groups, tickedCopies, expansion])

  return { hasGroups: groups.size > 0, describeRow }
}
