/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CopyProblem } from "@/lib/torrent-table/row-display"
import type { CrossInstanceTorrent, Torrent } from "@/types"

// The ids of the open Copy groups. The state lives in browser memory only.
export type CopyGroupExpansion = ReadonlySet<string>

export type CopyGroupExpansionEvent =
  | { type: "toggleGroup"; id: string }
  // The selection scope changed: instance, instance subset, filter, column filter, or search.
  | { type: "scopeChanged" }

export const COLLAPSED_COPY_GROUPS: CopyGroupExpansion = new Set()

export function copyGroupExpansionReducer(state: CopyGroupExpansion, event: CopyGroupExpansionEvent): CopyGroupExpansion {
  switch (event.type) {
    case "toggleGroup": {
      const open = new Set(state)
      if (!open.delete(event.id)) {
        open.add(event.id)
      }
      return open
    }
    case "scopeChanged":
      return state.size > 0 ? COLLAPSED_COPY_GROUPS : state
  }
}

// A member of a Copy group other than its Group representative is not a top row.
export function isTopRow(row: Pick<Torrent, "copy_group">): boolean {
  return !row.copy_group || row.copy_group.first_sorted !== undefined
}

function groupMembers<T extends Torrent>(rows: T[]): Map<string, T[]> {
  const groups = new Map<string, T[]>()
  for (const row of rows) {
    const id = row.copy_group?.id
    if (id === undefined) continue
    const members = groups.get(id)
    if (members) {
      members.push(row)
    } else {
      groups.set(id, [row])
    }
  }
  return groups
}

function topRowOf<T extends Torrent>(members: T[]): T {
  return members.find(isTopRow) ?? members[0]
}

/**
 * The rows that the list shows. A group takes the place of the first loaded row
 * with its id, because a stream merge can place a member apart from its group.
 * The row with first_sorted is on top, and an open group shows its other
 * members under it in loaded order. A list with no group returns `rows` itself.
 */
export function buildVisibleRows<T extends Torrent>(rows: T[], expansion: CopyGroupExpansion): T[] {
  const groups = groupMembers(rows)
  if (groups.size === 0) {
    return rows
  }

  const visible: T[] = []
  for (const row of rows) {
    const id = row.copy_group?.id
    if (id === undefined) {
      visible.push(row)
      continue
    }
    const members = groups.get(id)
    if (!members) continue
    groups.delete(id)
    const top = topRowOf(members)
    visible.push(top)
    if (expansion.has(id)) {
      for (const member of members) {
        if (member !== top) visible.push(member)
      }
    }
  }
  return visible
}

/** The ticked members other than the top row, for each group with one. */
export function countTickedCopies<T extends Torrent>(rows: T[], isTicked: (row: T) => boolean): Map<string, number> {
  const counts = new Map<string, number>()
  for (const [id, members] of groupMembers(rows)) {
    const top = topRowOf(members)
    const ticked = members.filter(member => member !== top && isTicked(member)).length
    if (ticked > 0) counts.set(id, ticked)
  }
  return counts
}

/**
 * What a closed top row shows: the top row with the sorted field of the
 * first-sorted member. A field that the row does not hold changes nothing.
 */
export function withSortedField<T extends Torrent>(top: T, firstSorted: T | undefined, sortedField: string | undefined): T {
  return firstSorted && sortedField && sortedField in top ? { ...top, [sortedField]: firstSorted[sortedField as keyof T] } : top
}

// Worst first.
const COPY_PROBLEMS: readonly CopyProblem[] = ["missingFiles", "error", "unregistered", "tracker_error", "tracker_down"]

function worstProblem(rows: Pick<Torrent, "state" | "tracker_health">[]): CopyProblem | undefined {
  let worst = COPY_PROBLEMS.length
  for (const row of rows) {
    for (const value of [row.state, row.tracker_health]) {
      const rank = COPY_PROBLEMS.indexOf(value as CopyProblem)
      if (rank !== -1 && rank < worst) worst = rank
    }
  }
  return COPY_PROBLEMS[worst]
}

interface CopyGroupSummary<T extends Torrent> {
  top: T
  /** The loaded members, the top row included. */
  size: number
  /** The worst problem of the members other than the top row. */
  hiddenProblem?: CopyProblem
  /** The member that placed the group in the sort. */
  firstSorted: T
}

export function summarizeCopyGroups<T extends Torrent>(rows: T[]): Map<string, CopyGroupSummary<T>> {
  const summaries = new Map<string, CopyGroupSummary<T>>()
  for (const [id, members] of groupMembers(rows)) {
    const top = topRowOf(members)
    const sorted = top.copy_group?.first_sorted
    // A row of one instance view has no instanceId: all its members are on that instance.
    const firstSorted = sorted && members.find(member => member.hash === sorted.hash &&
      ((member as Partial<CrossInstanceTorrent>).instanceId ?? sorted.instance_id) === sorted.instance_id)
    summaries.set(id, {
      top,
      size: members.length,
      hiddenProblem: worstProblem(members.filter(member => member !== top)),
      firstSorted: firstSorted || top,
    })
  }
  return summaries
}
