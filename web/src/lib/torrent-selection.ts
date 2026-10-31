/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import { combineFilterExpr } from "@/lib/torrent-filters"
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
function selectionKeyTargets(keys: Iterable<string>, isCrossInstance: boolean, instanceId: number): TorrentActionTarget[] {
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

// What a selection reaches beyond the bulk action itself.
export interface ActionTargetScope {
  instanceId: number
  // The instance subset of the Unified view.
  instanceIds?: number[]
  // The Sidebar filters.
  filters: TorrentFilters
  // Null on the mobile cards, which have no Column filters. inBrowser is set in
  // cross-seed filter mode, where the table and not the backend applies them.
  columnFilters: { expr: string | null; inBrowser: boolean } | null
  search: string
  // True when the request goes to the cross-instance endpoint: the Unified view and cross-seed filter mode.
  isCrossInstance: boolean
}

// The select-all part of an Action target: one form of Exclusions for each scope.
export interface SelectAllTarget {
  filters: TorrentFilters
  search: string
  instanceIds?: number[]
  excludeHashes?: string[]
  excludeTargets?: TorrentActionTarget[]
}

// What one bulk action reaches (ADR 0034). count is the number that the user
// sees. previewTorrents are the loaded torrents in the selection, for the
// delete dialog and the cross-seed warning.
export type ActionTarget = { count: number; previewTorrents: Torrent[] } & (
  // Plain hashes on one instance. On the cross-instance endpoint, instance-and-hash pairs and never a bare hash.
  | { kind: "ticks"; hashes: string[]; targets?: TorrentActionTarget[] }
  | { kind: "selectAll"; selectAll: SelectAllTarget }
)

export const EMPTY_ACTION_TARGET: ActionTarget = { kind: "ticks", hashes: [], count: 0, previewTorrents: [] }

// rows.loaded are the rows that the layout shows, rows.ticked the rows it remembers for each tick.
export function buildActionTarget(
  selection: TorrentSelectionState,
  scope: ActionTargetScope,
  rows: { loaded: Torrent[]; ticked: Torrent[]; totalCount: number }
): ActionTarget {
  const { instanceId, isCrossInstance, columnFilters } = scope
  if (!selection.isAllSelected) {
    const targets = selectionKeyTargets(selection.selected, isCrossInstance, instanceId)
    return {
      kind: "ticks",
      ...(isCrossInstance ? { hashes: [], targets } : { hashes: targets.map(target => target.hash) }),
      count: selection.selected.size,
      previewTorrents: rows.ticked,
    }
  }

  const previewTorrents = rows.loaded.filter(torrent => !selection.excluded.has(getSelectionIdentity(torrent, isCrossInstance, instanceId)))
  const excluded = selectionKeyTargets(selection.excluded, isCrossInstance, instanceId)
  return {
    kind: "selectAll",
    selectAll: {
      // The Column filters AND the filter expression (ADR 0010).
      filters: columnFilters ? { ...scope.filters, expr: combineFilterExpr(columnFilters.expr, scope.filters.expr) ?? "" } : scope.filters,
      search: scope.search,
      ...(isCrossInstance ? { instanceIds: scope.instanceIds, excludeTargets: excluded } : { excludeHashes: excluded.map(target => target.hash) }),
    },
    // The backend total does not know a Column filter that applies in the browser.
    // ponytail: counts loaded rows only, so a cross-seed set past one page
    // (300) under-reports; a cross-seed expression is one torrent's siblings.
    count: columnFilters?.inBrowser ? previewTorrents.length : Math.max(0, rows.totalCount - selection.excluded.size),
    previewTorrents,
  }
}

// The request fields of an Action target, for a bulk action and the tag editor baseline.
export function actionTargetRequest(target: ActionTarget) {
  if (target.kind === "selectAll") {
    return { hashes: [], selectAll: true, ...target.selectAll }
  }
  return { hashes: target.hashes, targets: target.targets, selectAll: false }
}
