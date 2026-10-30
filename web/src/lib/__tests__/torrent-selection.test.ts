/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent, TorrentFilters } from "@/types"
import {
  buildActionTarget,
  EMPTY_SELECTION,
  getSelectionIdentity,
  headerCheckboxEvent,
  rememberTickedTorrents,
  selectAllButtonEvent,
  selectAllCheckboxFlags,
  selectionCount,
  selectionReducer,
  selectionScope,
  type ActionTargetScope,
  type ActionTarget,
  type TorrentSelectionEvent,
  type TorrentSelectionState
} from "@/lib/torrent-selection"
import { describe, expect, it } from "vitest"

function run(...events: TorrentSelectionEvent[]): TorrentSelectionState {
  return events.reduce(selectionReducer, EMPTY_SELECTION)
}

function view(state: TorrentSelectionState) {
  return {
    isAllSelected: state.isAllSelected,
    selected: [...state.selected].sort(),
    excluded: [...state.excluded].sort(),
  }
}

describe("selectionReducer", () => {
  it.each<[string, TorrentSelectionEvent[], ReturnType<typeof view>]>([
    ["select all from nothing", [{ type: "selectAll" }], { isAllSelected: true, selected: [], excluded: [] }],
    ["tick", [{ type: "tick", keys: ["a"], checked: true }], { isAllSelected: false, selected: ["a"], excluded: [] }],
    [
      "untick",
      [{ type: "tick", keys: ["a", "b"], checked: true }, { type: "tick", keys: ["a"], checked: false }],
      { isAllSelected: false, selected: ["b"], excluded: [] },
    ],
    [
      "a range tick is one event",
      [{ type: "tick", keys: ["a", "b", "c"], checked: true }],
      { isAllSelected: false, selected: ["a", "b", "c"], excluded: [] },
    ],
    [
      "untick in Select all mode excludes",
      [{ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }],
      { isAllSelected: true, selected: [], excluded: ["a"] },
    ],
    [
      "re-tick in Select all mode removes the Exclusion",
      [{ type: "selectAll" }, { type: "tick", keys: ["a", "b"], checked: false }, { type: "tick", keys: ["a"], checked: true }],
      { isAllSelected: true, selected: [], excluded: ["b"] },
    ],
    [
      "select all drops earlier ticks",
      [{ type: "tick", keys: ["a"], checked: true }, { type: "selectAll" }],
      { isAllSelected: true, selected: [], excluded: [] },
    ],
    [
      "select only this row replaces the selection",
      [{ type: "tick", keys: ["a", "b"], checked: true }, { type: "selectOnly", key: "c" }],
      { isAllSelected: false, selected: ["c"], excluded: [] },
    ],
    [
      "select only this row leaves Select all mode",
      [{ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }, { type: "selectOnly", key: "c" }],
      { isAllSelected: false, selected: ["c"], excluded: [] },
    ],
    [
      "clear",
      [{ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }, { type: "clear" }],
      { isAllSelected: false, selected: [], excluded: [] },
    ],
    [
      "a scope change clears",
      [{ type: "scope", scope: "1" }, { type: "tick", keys: ["a"], checked: true }, { type: "scope", scope: "2" }],
      { isAllSelected: false, selected: [], excluded: [] },
    ],
    [
      "the same scope keeps the selection",
      [{ type: "scope", scope: "1" }, { type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }, { type: "scope", scope: "1" }],
      { isAllSelected: true, selected: [], excluded: ["a"] },
    ],
  ])("%s", (_name, events, expected) => {
    expect(view(run(...events))).toEqual(expected)
  })

  it("keeps the state object when clear has nothing to clear", () => {
    expect(selectionReducer(EMPTY_SELECTION, { type: "clear" })).toBe(EMPTY_SELECTION)
  })
})

describe("selectionScope", () => {
  it("ignores the sort, so a sort change does not clear", () => {
    const filters: TorrentFilters = { status: ["seeding"], excludeStatus: [], categories: [], excludeCategories: [], tags: [], excludeTags: [], trackers: [], excludeTrackers: [] }
    expect(selectionScope(1, undefined, filters, "ubuntu", [], false)).toBe(selectionScope(1, undefined, { ...filters }, "ubuntu", [], false))
  })

  it.each([
    ["instance", selectionScope(2, [1, 2], undefined, "", [], false)],
    ["instance subset of the Unified view", selectionScope(1, [1], undefined, "", [], false)],
    ["filters", selectionScope(1, [1, 2], { status: ["seeding"] } as TorrentFilters, "", [], false)],
    ["search", selectionScope(1, [1, 2], undefined, "ubuntu", [], false)],
    ["column filters", selectionScope(1, [1, 2], undefined, "", [{ columnId: "name", operation: "contains", value: "ubuntu" }], false)],
    ["layout", selectionScope(1, [1, 2], undefined, "", [], true)],
  ])("changes with the %s", (_name, other) => {
    expect(other).not.toBe(selectionScope(1, [1, 2], undefined, "", [], false))
  })
})

describe("control rules", () => {
  const ticked = run({ type: "tick", keys: ["a"], checked: true })
  const all = run({ type: "selectAll" })

  it.each<[string, TorrentSelectionState, TorrentSelectionEvent["type"]]>([
    ["nothing selected", EMPTY_SELECTION, "selectAll"],
    ["one row ticked", ticked, "clear"],
    ["Select all mode", all, "clear"],
  ])("desktop header checkbox with %s sends %s", (_name, state, expected) => {
    expect(headerCheckboxEvent(state).type).toBe(expected)
  })

  it.each<[string, TorrentSelectionState, number, TorrentSelectionEvent["type"]]>([
    ["one of three ticked", ticked, 3, "selectAll"],
    ["every torrent ticked", ticked, 1, "clear"],
    ["Select all mode", all, 3, "clear"],
    ["Select all mode with an Exclusion", run({ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }), 3, "selectAll"],
  ])("mobile Select all button with %s sends %s", (_name, state, total, expected) => {
    expect(selectAllButtonEvent(state, total).type).toBe(expected)
  })
})

describe("selectionCount", () => {
  it.each<[string, TorrentSelectionState, number, number]>([
    ["nothing", EMPTY_SELECTION, 10, 0],
    ["ticks", run({ type: "tick", keys: ["a", "b"], checked: true }), 10, 2],
    ["Select all mode", run({ type: "selectAll" }), 10, 10],
    ["Select all mode minus Exclusions", run({ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }), 10, 9],
  ])("%s", (_name, state, total, expected) => {
    expect(selectionCount(state, total)).toBe(expected)
  })
})

describe("selectAllCheckboxFlags", () => {
  const rows = ["a", "b", "c"]
  it.each<[string, TorrentSelectionState, string[], { checked: boolean; indeterminate: boolean }]>([
    ["nothing", EMPTY_SELECTION, rows, { checked: false, indeterminate: false }],
    ["some ticked", run({ type: "tick", keys: ["a"], checked: true }), rows, { checked: false, indeterminate: true }],
    ["every row ticked", run({ type: "tick", keys: rows, checked: true }), rows, { checked: true, indeterminate: false }],
    ["as many ticks as rows, on rows that are not loaded", run({ type: "tick", keys: ["x", "y", "z"], checked: true }), rows, { checked: false, indeterminate: true }],
    ["more ticks than rows", run({ type: "tick", keys: ["a", "b", "c", "x"], checked: true }), rows, { checked: true, indeterminate: false }],
    ["ticks and an empty list", run({ type: "tick", keys: ["x"], checked: true }), [], { checked: false, indeterminate: true }],
    ["empty list", EMPTY_SELECTION, [], { checked: false, indeterminate: false }],
    ["Select all mode", run({ type: "selectAll" }), rows, { checked: true, indeterminate: false }],
    ["Select all mode with an Exclusion", run({ type: "selectAll" }, { type: "tick", keys: ["a"], checked: false }), rows, { checked: false, indeterminate: true }],
  ])("%s", (_name, state, rows, expected) => {
    expect(selectAllCheckboxFlags(state, rows)).toEqual(expected)
  })
})

describe("getSelectionIdentity", () => {
  it("returns the bare hash for a single instance", () => {
    expect(getSelectionIdentity(makeTorrent({ hash: "abc" }), false, 5)).toBe("abc")
  })

  it("qualifies the hash with the torrent's instance in the Unified view", () => {
    const torrent = { ...makeTorrent({ hash: "abc" }), instanceId: 9 } as Torrent
    expect(getSelectionIdentity(torrent, true, 5)).toBe("9:abc")
  })

  it("falls back to the view's instance when the torrent has none", () => {
    expect(getSelectionIdentity(makeTorrent({ hash: "abc" }), true, 5)).toBe("5:abc")
  })
})

describe("buildActionTarget", () => {
  const cross = (hash: string, instanceId: number) => ({ ...makeTorrent({ hash }), instanceId }) as Torrent
  const [a, b, c, z] = ["a", "b", "c", "z"].map(hash => makeTorrent({ hash }))
  const [s1, s2, d1, z2] = [cross("s", 1), cross("s", 2), cross("d", 1), cross("z", 2)]
  const [t2] = [cross("t", 2)]
  const total = 7500
  const sidebar: TorrentFilters = { status: ["active"], excludeStatus: [], categories: [], excludeCategories: [], tags: [], excludeTags: [], trackers: [], excludeTrackers: [] }
  const crossSeedExpr = "Hash == \"s\" || Hash == \"t\""

  const single: ActionTargetScope = { instanceId: 5, filters: sidebar, columnFilters: { expr: null, inBrowser: false }, search: "x", isCrossInstance: false }
  const unified: ActionTargetScope = { instanceId: 0, instanceIds: [1, 2], filters: sidebar, columnFilters: { expr: null, inBrowser: false }, search: "x", isCrossInstance: true }
  const crossSeed: ActionTargetScope = { instanceId: 1, filters: { ...sidebar, expr: crossSeedExpr }, columnFilters: { expr: "Ratio > 1", inBrowser: true }, search: "", isCrossInstance: true }
  const mobile: ActionTargetScope = { instanceId: 5, filters: sidebar, columnFilters: null, search: "x", isCrossInstance: false }
  const columnFilter: ActionTargetScope = { ...single, filters: { ...sidebar, expr: "Category == \"tv\"" }, columnFilters: { expr: "Size > 1", inBrowser: false } }

  const ticks = (...keys: string[]) => run({ type: "tick", keys, checked: true })
  const selectAll = (...excluded: string[]) => run({ type: "selectAll" }, { type: "tick", keys: excluded, checked: false })

  it.each<[string, ActionTargetScope, TorrentSelectionState, { loaded: Torrent[]; ticked: Torrent[] }, ActionTarget]>([
    ["single instance: ticks", single, ticks("a", "b"), { loaded: [a, b, c], ticked: [a, b] },
      { kind: "ticks", hashes: ["a", "b"], count: 2, previewTorrents: [a, b] }],
    ["single instance: a tick that is no longer loaded", single, ticks("a", "z"), { loaded: [a, b, c], ticked: [a, z] },
      { kind: "ticks", hashes: ["a", "z"], count: 2, previewTorrents: [a, z] }],
    ["single instance: Select all mode", single, selectAll(), { loaded: [a, b, c], ticked: [] },
      { kind: "selectAll", selectAll: { filters: { ...sidebar, expr: "" }, search: "x", excludeHashes: [] }, count: 7500, previewTorrents: [a, b, c] }],
    ["single instance: Select all mode with 2 Exclusions", single, selectAll("b", "z"), { loaded: [a, b, c], ticked: [] },
      { kind: "selectAll", selectAll: { filters: { ...sidebar, expr: "" }, search: "x", excludeHashes: ["b", "z"] }, count: 7498, previewTorrents: [a, c] }],

    ["Unified view: ticks name the instance of a shared hash", unified, ticks("1:s"), { loaded: [s1, s2, d1], ticked: [s1] },
      { kind: "ticks", hashes: [], targets: [{ instanceId: 1, hash: "s" }], count: 1, previewTorrents: [s1] }],
    ["Unified view: a tick that is no longer loaded", unified, ticks("1:s", "2:z"), { loaded: [s1, s2, d1], ticked: [s1, z2] },
      { kind: "ticks", hashes: [], targets: [{ instanceId: 1, hash: "s" }, { instanceId: 2, hash: "z" }], count: 2, previewTorrents: [s1, z2] }],
    ["Unified view: Select all mode", unified, selectAll(), { loaded: [s1, s2, d1], ticked: [] },
      { kind: "selectAll", selectAll: { filters: { ...sidebar, expr: "" }, search: "x", instanceIds: [1, 2], excludeTargets: [] }, count: 7500, previewTorrents: [s1, s2, d1] }],
    ["Unified view: Select all mode with 2 Exclusions", unified, selectAll("2:s", "1:z"), { loaded: [s1, s2, d1], ticked: [] },
      {
        kind: "selectAll",
        selectAll: { filters: { ...sidebar, expr: "" }, search: "x", instanceIds: [1, 2], excludeTargets: [{ instanceId: 2, hash: "s" }, { instanceId: 1, hash: "z" }] },
        count: 7498,
        previewTorrents: [s1, d1],
      }],

    ["cross-seed filter mode: ticks", crossSeed, ticks("1:s", "2:t"), { loaded: [s1, t2], ticked: [s1, t2] },
      { kind: "ticks", hashes: [], targets: [{ instanceId: 1, hash: "s" }, { instanceId: 2, hash: "t" }], count: 2, previewTorrents: [s1, t2] }],
    ["cross-seed filter mode: a tick that is no longer loaded", crossSeed, ticks("2:z"), { loaded: [s1, t2], ticked: [z2] },
      { kind: "ticks", hashes: [], targets: [{ instanceId: 2, hash: "z" }], count: 1, previewTorrents: [z2] }],
    ["cross-seed filter mode: Select all mode counts the loaded rows that match", crossSeed, selectAll(), { loaded: [s1, t2], ticked: [] },
      {
        kind: "selectAll",
        selectAll: { filters: { ...sidebar, expr: `(Ratio > 1) && (${crossSeedExpr})` }, search: "", excludeTargets: [] },
        count: 2,
        previewTorrents: [s1, t2],
      }],
    ["cross-seed filter mode: Select all mode with 2 Exclusions", crossSeed, selectAll("2:t", "3:u"), { loaded: [s1, t2], ticked: [] },
      {
        kind: "selectAll",
        selectAll: { filters: { ...sidebar, expr: `(Ratio > 1) && (${crossSeedExpr})` }, search: "", excludeTargets: [{ instanceId: 2, hash: "t" }, { instanceId: 3, hash: "u" }] },
        count: 1,
        previewTorrents: [s1],
      }],

    ["mobile cards: Select all mode sends the Sidebar filters with no Column filters", mobile, selectAll("b"), { loaded: [a, b, c], ticked: [] },
      { kind: "selectAll", selectAll: { filters: sidebar, search: "x", excludeHashes: ["b"] }, count: 7499, previewTorrents: [a, c] }],
    ["desktop: Select all mode joins the Column filter to the filter expression", columnFilter, selectAll(), { loaded: [a], ticked: [] },
      {
        kind: "selectAll",
        selectAll: { filters: { ...sidebar, expr: "(Size > 1) && (Category == \"tv\")" }, search: "x", excludeHashes: [] },
        count: 7500,
        previewTorrents: [a],
      }],
  ])("%s", (_name, scope, selection, rows, expected) => {
    expect(buildActionTarget(selection, scope, { ...rows, totalCount: total })).toEqual(expected)
  })
})

describe("rememberTickedTorrents", () => {
  const a = makeTorrent({ hash: "a" })
  const b = makeTorrent({ hash: "b" })
  const key = (torrent: Torrent) => torrent.hash

  it("keeps a ticked row after a sort moves it past the loaded rows, and drops it on untick", () => {
    const cache = new Map<string, Torrent>()
    expect(rememberTickedTorrents(cache, new Set(["a"]), [a, b], key)).toEqual([a])
    expect(rememberTickedTorrents(cache, new Set(["a"]), [b], key)).toEqual([a])
    expect(rememberTickedTorrents(cache, new Set(["b"]), [b], key)).toEqual([b])
    expect(rememberTickedTorrents(cache, new Set(), [a, b], key)).toEqual([])
  })
})
