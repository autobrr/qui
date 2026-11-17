/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferBulkAction, TransferFilter, TransferRow } from "@/lib/transfers-api"
import { canCancel, canRemove, canRetry } from "./transfer-display"

/** The rows that the user picked: rows of the page that shows, or every row behind the filter chip, on every page. */
export type Selection = { kind: "rows"; ids: ReadonlySet<number> } | { kind: "filter" }

export const noSelection: Selection = { kind: "rows", ids: new Set() }

const fits: Record<TransferBulkAction, (row: TransferRow) => boolean> = { retry: canRetry, cancel: canCancel, remove: canRemove }

/** The steps that fit some row behind each chip. The page does not know the rows of the other pages. */
const chipActions: Record<TransferFilter, readonly TransferBulkAction[]> = {
  all: ["retry", "cancel", "remove"],
  active: ["cancel"],
  waiting: ["cancel"],
  finished: ["retry", "remove"],
  failed: ["retry", "cancel", "remove"],
}

/** The steps that the selection bar offers: each step that fits one or more of the picked rows. */
export function offeredActions(selection: Selection, filter: TransferFilter, picked: readonly TransferRow[]): readonly TransferBulkAction[] {
  if (selection.kind === "filter") return chipActions[filter]
  return (["retry", "cancel", "remove"] as const).filter(action => picked.some(fits[action]))
}

/** The ids of the picked rows that the step fits. */
export function fittingIds(action: TransferBulkAction, picked: readonly TransferRow[]): number[] {
  return picked.filter(fits[action]).map(row => row.id)
}

/**
 * The selection after a tick on the row at index. With range, the rows from the last ticked row up to this one take
 * the new state of this row, as in a file manager.
 */
export function tickRow(selection: Selection, rows: readonly TransferRow[], index: number, last: number | null): Selection {
  const on = !(selection.kind === "filter" || selection.ids.has(rows[index].id))
  // A tick in a selection of the whole chip keeps the rows of this page only.
  const ids = new Set(selection.kind === "filter" ? rows.map(row => row.id) : selection.ids)
  const from = last === null ? index : Math.min(last, index)
  const to = last === null ? index : Math.max(last, index)
  for (const row of rows.slice(from, to + 1)) {
    if (on) ids.add(row.id)
    else ids.delete(row.id)
  }
  return { kind: "rows", ids }
}
