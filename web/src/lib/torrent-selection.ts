/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { CrossInstanceTorrent, Torrent, TorrentFilters } from "@/types"

// The torrent list selection, shared by the desktop table and the mobile cards.
// Keys come from getSelectionIdentity. ADR 0026 records why the reducer never
// prunes a key that is no longer visible.

export interface TorrentSelectionState {
  isAllSelected: boolean
  // Ticked keys outside Select all mode.
  selected: ReadonlySet<string>
  // Exclusions: keys the user unticked in Select all mode.
  excluded: ReadonlySet<string>
  // The instance, instance subset, filters, search, and layout that the selection belongs to.
  scope: string
}

export type TorrentSelectionEvent =
  | { type: "tick"; keys: string[]; checked: boolean }
  | { type: "selectOnly"; key: string }
  | { type: "selectAll" }
  | { type: "clear" }
  | { type: "scope"; scope: string }

export const EMPTY_SELECTION: TorrentSelectionState = {
  isAllSelected: false,
  selected: new Set(),
  excluded: new Set(),
  scope: "",
}

function withKeys(set: ReadonlySet<string>, keys: string[], add: boolean): Set<string> {
  const next = new Set(set)
  for (const key of keys) {
    if (add) {
      next.add(key)
    } else {
      next.delete(key)
    }
  }
  return next
}

export function selectionReducer(state: TorrentSelectionState, event: TorrentSelectionEvent): TorrentSelectionState {
  switch (event.type) {
    case "tick":
      if (state.isAllSelected) {
        return { ...state, excluded: withKeys(state.excluded, event.keys, !event.checked) }
      }
      return { ...state, selected: withKeys(state.selected, event.keys, event.checked) }
    case "selectOnly":
      return { ...state, isAllSelected: false, selected: new Set([event.key]), excluded: new Set() }
    case "selectAll":
      return { ...state, isAllSelected: true, selected: new Set(), excluded: new Set() }
    case "clear":
      return hasSelection(state) ? { ...EMPTY_SELECTION, scope: state.scope } : state
    case "scope":
      return event.scope === state.scope ? state : { ...EMPTY_SELECTION, scope: event.scope }
  }
}

// The sort is not part of the scope: a sort change keeps the selection.
// instanceIds is the instance subset of the Unified view. The layout is part of
// the scope because the mobile cards ignore column filters and keep their own
// search, so a select-all request from the other layout would reach other torrents.
export function selectionScope(
  instanceId: number,
  instanceIds: number[] | undefined,
  filters: TorrentFilters | undefined,
  search: string,
  columnFilters: ColumnFilter[],
  isMobile: boolean
): string {
  return JSON.stringify([instanceId, instanceIds ?? null, filters ?? null, search, columnFilters, isMobile])
}

export function hasSelection(state: TorrentSelectionState): boolean {
  return state.isAllSelected || state.selected.size > 0
}

export function isKeySelected(state: TorrentSelectionState, key: string): boolean {
  return state.isAllSelected ? !state.excluded.has(key) : state.selected.has(key)
}

// Stale keys count too, so the number can be off by them. It is display only.
export function selectionCount(state: TorrentSelectionState, totalCount: number): number {
  return state.isAllSelected ? Math.max(0, totalCount - state.excluded.size) : state.selected.size
}

// rowKeys are the keys of the loaded rows. A tick can belong to a row that is
// not loaded, so the ticks count alone cannot tell whether every row is ticked.
export function selectAllCheckboxFlags(state: TorrentSelectionState, rowKeys: string[]) {
  if (state.isAllSelected) {
    return { checked: state.excluded.size === 0, indeterminate: state.excluded.size > 0 }
  }
  const checked = state.selected.size > 0 && rowKeys.length > 0 && rowKeys.every(key => state.selected.has(key))
  return { checked, indeterminate: state.selected.size > 0 && !checked }
}

// Desktop header checkbox: if anything is selected, clear. Otherwise, select all.
export function headerCheckboxEvent(state: TorrentSelectionState): TorrentSelectionEvent {
  return hasSelection(state) ? { type: "clear" } : { type: "selectAll" }
}

// Mobile "Select all" button: if everything is selected, clear. Otherwise, select all.
// The button shows only when something is selected, so the header rule would make it a clear button.
export function selectAllButtonEvent(state: TorrentSelectionState, totalCount: number): TorrentSelectionEvent {
  return selectionCount(state, totalCount) === totalCount ? { type: "clear" } : { type: "selectAll" }
}

// The hash, or `instanceId:hash` when the view spans instances.
export function getSelectionIdentity(torrent: Torrent, isCrossInstance: boolean, instanceId: number): string {
  if (!isCrossInstance) {
    return torrent.hash
  }

  const crossInstanceId = (torrent as Partial<CrossInstanceTorrent>).instanceId
  const resolvedInstanceId = typeof crossInstanceId === "number" && crossInstanceId > 0 ? crossInstanceId : instanceId
  return `${resolvedInstanceId}:${torrent.hash}`
}

// Keeps the last row seen for each tick in cache and returns the ticked rows.
// A sort, or a live value such as speed in the sort key, can move a ticked
// torrent past the loaded rows. An action that needs the row, such as an
// export, must still reach it.
export function rememberTickedTorrents(
  cache: Map<string, Torrent>,
  selectedKeys: ReadonlySet<string>,
  rows: Torrent[],
  getKey: (torrent: Torrent) => string
): Torrent[] {
  for (const key of cache.keys()) {
    if (!selectedKeys.has(key)) {
      cache.delete(key)
    }
  }
  for (const row of rows) {
    const key = getKey(row)
    if (selectedKeys.has(key)) {
      cache.set(key, row)
    }
  }
  return [...cache.values()]
}

// The request targets for selection keys, so a key reaches its torrent when the row is not loaded.
export function getSelectionTargets(keys: Iterable<string>, isCrossInstance: boolean, instanceId: number): TorrentActionTarget[] {
  const targets: TorrentActionTarget[] = []
  for (const key of keys) {
    if (!isCrossInstance) {
      targets.push({ instanceId, hash: key })
      continue
    }
    const separator = key.indexOf(":")
    const targetInstanceId = Number.parseInt(key.slice(0, separator), 10)
    const hash = key.slice(separator + 1)
    if (targetInstanceId > 0 && hash !== "") {
      targets.push({ instanceId: targetInstanceId, hash })
    }
  }
  return targets
}
