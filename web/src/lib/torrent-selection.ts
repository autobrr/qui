/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import { combineFilterExpr } from "@/lib/torrent-filters"
import type { CrossInstanceTorrent, Torrent, TorrentFilters } from "@/types"

export interface TorrentSelectionState {
  isAllSelected: boolean
  selected: ReadonlySet<string>
  excluded: ReadonlySet<string>
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

export function selectionCount(state: TorrentSelectionState, totalCount: number): number {
  return state.isAllSelected ? Math.max(0, totalCount - state.excluded.size) : state.selected.size
}

export function selectAllCheckboxFlags(state: TorrentSelectionState, rowKeys: string[]) {
  if (state.isAllSelected) {
    return { checked: state.excluded.size === 0, indeterminate: state.excluded.size > 0 }
  }
  const checked = state.selected.size > 0 && rowKeys.length > 0 && rowKeys.every(key => state.selected.has(key))
  return { checked, indeterminate: state.selected.size > 0 && !checked }
}

export function headerCheckboxEvent(state: TorrentSelectionState): TorrentSelectionEvent {
  return hasSelection(state) ? { type: "clear" } : { type: "selectAll" }
}

export function selectAllButtonEvent(state: TorrentSelectionState, totalCount: number): TorrentSelectionEvent {
  return selectionCount(state, totalCount) === totalCount ? { type: "clear" } : { type: "selectAll" }
}

export function getSelectionIdentity(torrent: Torrent, isCrossInstance: boolean, instanceId: number): string {
  if (!isCrossInstance) {
    return torrent.hash
  }

  const crossInstanceId = (torrent as Partial<CrossInstanceTorrent>).instanceId
  const resolvedInstanceId = typeof crossInstanceId === "number" && crossInstanceId > 0 ? crossInstanceId : instanceId
  return `${resolvedInstanceId}:${torrent.hash}`
}

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

export interface ActionTargetScope {
  instanceId: number
  instanceIds?: number[]
  filters: TorrentFilters
  columnFilters: { expr: string | null; inBrowser: boolean } | null
  search: string
  isCrossInstance: boolean
}

export interface SelectAllTarget {
  filters: TorrentFilters
  search: string
  instanceIds?: number[]
  excludeHashes?: string[]
  excludeTargets?: TorrentActionTarget[]
}

export type ActionTarget = { count: number; previewTorrents: Torrent[] } & (
  | { kind: "ticks"; hashes: string[]; targets?: TorrentActionTarget[] }
  | { kind: "selectAll"; selectAll: SelectAllTarget }
)

export const EMPTY_ACTION_TARGET: ActionTarget = { kind: "ticks", hashes: [], count: 0, previewTorrents: [] }

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
      filters: columnFilters ? { ...scope.filters, expr: combineFilterExpr(columnFilters.expr, scope.filters.expr) ?? "" } : scope.filters,
      search: scope.search,
      ...(isCrossInstance ? { instanceIds: scope.instanceIds, excludeTargets: excluded } : { excludeHashes: excluded.map(target => target.hash) }),
    },
    count: columnFilters?.inBrowser ? previewTorrents.length : Math.max(0, rows.totalCount - selection.excluded.size),
    previewTorrents,
  }
}

export function actionTargetInstanceIds(target: ActionTarget, instanceId: number): number[] | undefined {
  if (target.kind === "ticks" && target.targets?.length) {
    return [...new Set(target.targets.map(t => t.instanceId))]
  }
  if (instanceId > 0) {
    return [instanceId]
  }
  if (target.kind === "selectAll") {
    return target.selectAll.instanceIds
  }
  return []
}

export function actionTargetRequest(target: ActionTarget) {
  if (target.kind === "selectAll") {
    return { hashes: [], selectAll: true, ...target.selectAll }
  }
  return { hashes: target.hashes, targets: target.targets, selectAll: false }
}
