/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferBulkAction, TransferFilter, TransferRow } from "@/lib/transfers-api"
import { canCancel, canRemove, canRetry } from "./transfer-display"

export type Selection = { kind: "rows"; ids: ReadonlySet<number> } | { kind: "filter" }

export const noSelection: Selection = { kind: "rows", ids: new Set() }

const fits: Record<TransferBulkAction, (row: TransferRow) => boolean> = { retry: canRetry, cancel: canCancel, remove: canRemove }

export type ChipAction = Exclude<TransferBulkAction, "retry">

const chipActions: Record<TransferFilter, readonly ChipAction[]> = {
  all: ["cancel", "remove"],
  active: ["cancel"],
  waiting: ["cancel"],
  finished: ["remove"],
  failed: ["cancel", "remove"],
}

export function offeredActions(selection: Selection, filter: TransferFilter, picked: readonly TransferRow[]): readonly TransferBulkAction[] {
  if (selection.kind === "filter") return chipActions[filter]
  return (["retry", "cancel", "remove"] as const).filter(action => picked.some(fits[action]))
}

export function fittingIds(action: TransferBulkAction, picked: readonly TransferRow[]): number[] {
  return picked.filter(fits[action]).map(row => row.id)
}

export function tickRow(selection: Selection, rows: readonly TransferRow[], index: number, anchor: number | null): Selection {
  const on = !(selection.kind === "filter" || selection.ids.has(rows[index].id))
  const ids = new Set(selection.kind === "filter" ? rows.map(row => row.id) : selection.ids)
  const last = anchor === null ? -1 : rows.findIndex(row => row.id === anchor)
  const from = last === -1 ? index : Math.min(last, index)
  const to = last === -1 ? index : Math.max(last, index)
  for (const row of rows.slice(from, to + 1)) {
    if (on) ids.add(row.id)
    else ids.delete(row.id)
  }
  return { kind: "rows", ids }
}
