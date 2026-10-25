/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
  // The instance, instance subset, filters, and search that the selection belongs to.
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
// instanceIds is the instance subset of the Unified view.
export function selectionScope(
  instanceId: number,
  instanceIds: number[] | undefined,
  filters: TorrentFilters | undefined,
  search: string
): string {
  return JSON.stringify([instanceId, instanceIds ?? null, filters ?? null, search])
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

export function selectAllCheckboxFlags(state: TorrentSelectionState, rowCount: number) {
  if (state.isAllSelected) {
    return { checked: state.excluded.size === 0, indeterminate: state.excluded.size > 0 }
  }
  const count = state.selected.size
  return { checked: count > 0 && count === rowCount, indeterminate: count > 0 && count < rowCount }
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
