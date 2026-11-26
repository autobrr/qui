/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { countTickedCopies, summarizeCopyGroups, type CopyGroupExpansion } from "@/lib/copy-groups"
import type { CopyProblem } from "@/lib/torrent-table/row-display"
import type { TorrentSelectionState } from "@/lib/torrent-selection"
import type { Torrent } from "@/types"
import { useCallback, useMemo } from "react"

export interface CopyGroupRow<T extends Torrent> {
  groupId?: string
  open: boolean
  copies: number
  hiddenProblem?: CopyProblem
  hiddenTicks: number
  sortedTorrent?: T
  nestedCopy: boolean
}

const PLAIN_ROW = { open: false, copies: 0, hiddenTicks: 0, nestedCopy: false } as const
const NESTED_COPY = { ...PLAIN_ROW, nestedCopy: true } as const
const NO_TICKED_COPIES: ReadonlyMap<string, number> = new Map()

export interface UseCopyGroupRowsParams<T extends Torrent> {
  rows: T[]
  expansion: CopyGroupExpansion
  selection: TorrentSelectionState
  getSelectionIdentity: (row: T) => string
}

export function useCopyGroupRows<T extends Torrent>({ rows, expansion, selection, getSelectionIdentity }: UseCopyGroupRowsParams<T>) {
  const groups = useMemo(() => summarizeCopyGroups(rows), [rows])

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
    const open = expansion.has(id)
    return {
      groupId: id,
      open,
      copies: group.size - 1,
      hiddenProblem: open ? undefined : group.hiddenProblem,
      hiddenTicks: open ? 0 : tickedCopies.get(id) ?? 0,
      sortedTorrent: !open && group.firstSorted !== row ? group.firstSorted : undefined,
      nestedCopy: false,
    }
  }, [groups, tickedCopies, expansion])

  return { hasGroups: groups.size > 0, describeRow }
}
