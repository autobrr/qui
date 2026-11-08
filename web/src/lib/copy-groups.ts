/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossInstanceTorrent, Torrent } from "@/types"

// Which Copy groups are open. Expand all is one flag; toggled holds the groups
// that differ from it. The state lives in browser memory only.
export interface CopyGroupExpansion {
  expandAll: boolean
  toggled: ReadonlySet<string>
}

export type CopyGroupExpansionEvent =
  | { type: "toggleGroup"; id: string }
  | { type: "toggleExpandAll" }
  // The selection scope changed: instance, instance subset, filter, column filter, or search.
  | { type: "scopeChanged" }

export const COLLAPSED_COPY_GROUPS: CopyGroupExpansion = { expandAll: false, toggled: new Set() }

export function copyGroupExpansionReducer(state: CopyGroupExpansion, event: CopyGroupExpansionEvent): CopyGroupExpansion {
  switch (event.type) {
    case "toggleGroup": {
      const toggled = new Set(state.toggled)
      if (!toggled.delete(event.id)) {
        toggled.add(event.id)
      }
      return { ...state, toggled }
    }
    case "toggleExpandAll":
      return { expandAll: !state.expandAll, toggled: new Set() }
    case "scopeChanged":
      return state.expandAll || state.toggled.size > 0 ? COLLAPSED_COPY_GROUPS : state
  }
}

// A member of a Copy group other than its Group representative is not a top row.
export function isTopRow(row: Pick<Torrent, "copy_group">): boolean {
  return !row.copy_group || row.copy_group.first_sorted !== undefined
}

export function isCopyGroupOpen(expansion: CopyGroupExpansion, id: string): boolean {
  return expansion.expandAll !== expansion.toggled.has(id)
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
    if (isCopyGroupOpen(expansion, id)) {
      for (const member of members) {
        if (member !== top) visible.push(member)
      }
    }
  }
  return visible
}

function hasCopyProblem(row: Pick<Torrent, "state" | "tracker_health">): boolean {
  return row.state === "error" || row.state === "missingFiles" || row.tracker_health !== undefined
}

interface CopyGroupSummary<T extends Torrent> {
  top: T
  /** The loaded members, the top row included. */
  size: number
  /** A member other than the top row has a problem. */
  hiddenProblem: boolean
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
      hiddenProblem: members.some(member => member !== top && hasCopyProblem(member)),
      firstSorted: firstSorted || top,
    })
  }
  return summaries
}
