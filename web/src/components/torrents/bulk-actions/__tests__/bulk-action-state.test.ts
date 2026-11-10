/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { bulkDialogReducer, bulkRequest, CLOSED_DIALOG, deleteSize, dialogLists, dialogPick, resolvePick, type BulkActionPick, type BulkDialog } from "../bulk-action-state"
import type { ActionTarget } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"
import { LIST_FACTS } from "@/test/mockListFacts"
import { makeTorrent } from "@/test/mockTorrent"

describe("resolvePick", () => {
  it("runs a pick with no confirmation at once, with its option", () => {
    for (const pick of [{ action: "pause" }, { action: "forceStart", enable: true }] as const) {
      expect(resolvePick(pick, 40)).toEqual({ kind: "run", pick })
    }
  })

  it("asks before Recheck and Reannounce on more than 1 torrent", () => {
    expect(resolvePick({ action: "recheck" }, 2)).toEqual({ kind: "open", dialog: { name: "recheck" } })
    expect(resolvePick({ action: "reannounce" }, 2)).toEqual({ kind: "open", dialog: { name: "reannounce" } })
  })

  it("runs Recheck and Reannounce on 1 torrent at once", () => {
    expect(resolvePick({ action: "recheck" }, 1)).toEqual({ kind: "run", pick: { action: "recheck" } })
    expect(resolvePick({ action: "reannounce" }, 1)).toEqual({ kind: "run", pick: { action: "reannounce" } })
  })

  it("asks before automatic management with the direction, also for 1 torrent", () => {
    expect(resolvePick({ action: "toggleAutoTMM", enable: false }, 1)).toEqual({ kind: "open", dialog: { name: "toggleAutoTMM", enable: false } })
  })

  it("opens the delete dialog, also for 1 torrent", () => {
    expect(resolvePick({ action: "delete" }, 1)).toEqual({ kind: "open", dialog: { name: "delete", deleteCrossSeeds: false } })
  })

  it("opens the dialog of a Bulk action with values", () => {
    for (const action of ["setTags", "setCategory", "setShareLimit", "setSpeedLimits", "setComment", "renameTorrent"] as const) {
      expect(resolvePick({ action } as BulkActionPick, 1)).toEqual({ kind: "open", dialog: { name: action } })
    }
  })

  it("runs a category from the category submenu at once", () => {
    expect(resolvePick({ action: "setCategory", category: "movies" }, 40)).toEqual({ kind: "run", pick: { action: "setCategory", category: "movies" } })
    expect(resolvePick({ action: "setCategory", category: "" }, 40)).toEqual({ kind: "run", pick: { action: "setCategory", category: "" } })
  })

  it("opens the warning before the location dialog", () => {
    expect(resolvePick({ action: "setLocation" }, 1)).toEqual({ kind: "open", dialog: { name: "locationWarning" } })
  })

  it("exports with no dialog", () => {
    expect(resolvePick({ action: "export" }, 40)).toEqual({ kind: "export" })
  })
})

describe("bulkDialogReducer", () => {
  const recheck: BulkDialog = { name: "recheck" }

  it("opens the named dialog in place of another one", () => {
    const state = bulkDialogReducer(recheck, { type: "open", dialog: { name: "toggleAutoTMM", enable: true } })
    expect(state).toEqual({ name: "toggleAutoTMM", enable: true })
  })

  it("closes on a request of its own action, and stays open on another one", () => {
    expect(bulkDialogReducer(recheck, { type: "requestSucceeded", action: "recheck" })).toBe(CLOSED_DIALOG)
    expect(bulkDialogReducer(recheck, { type: "requestSucceeded", action: "pause" })).toBe(recheck)
  })

  it("starts each open of the delete dialog with \"also delete cross-seeds\" cleared", () => {
    const openDelete = (state: BulkDialog) => {
      const outcome = resolvePick({ action: "delete" }, 2)
      if (outcome.kind !== "open") throw new Error("delete must open a dialog")
      return bulkDialogReducer(state, { type: "open", dialog: outcome.dialog })
    }
    const checked = bulkDialogReducer(openDelete(CLOSED_DIALOG), { type: "deleteCrossSeeds", checked: true })
    expect(checked).toEqual({ name: "delete", deleteCrossSeeds: true })

    expect(openDelete(checked)).toEqual({ name: "delete", deleteCrossSeeds: false })
    expect(openDelete(bulkDialogReducer(checked, { type: "close" }))).toEqual({ name: "delete", deleteCrossSeeds: false })
  })

  it("keeps \"also delete cross-seeds\" out of the other dialogs", () => {
    expect(bulkDialogReducer(recheck, { type: "deleteCrossSeeds", checked: true })).toBe(recheck)
  })

  it("opens the location dialog when the user confirms the warning", () => {
    const warning = bulkDialogReducer(CLOSED_DIALOG, { type: "open", dialog: { name: "locationWarning" } })
    expect(bulkDialogReducer(warning, { type: "locationWarningConfirmed" })).toEqual({ name: "setLocation" })
  })

  it("opens no location dialog without the warning", () => {
    expect(bulkDialogReducer(CLOSED_DIALOG, { type: "locationWarningConfirmed" })).toBe(CLOSED_DIALOG)
    expect(bulkDialogReducer(recheck, { type: "locationWarningConfirmed" })).toBe(recheck)
  })

  it("closes a dialog with values on a request of its own action", () => {
    const category: BulkDialog = { name: "setCategory" }
    expect(bulkDialogReducer(category, { type: "requestSucceeded", action: "setCategory" })).toBe(CLOSED_DIALOG)
    expect(bulkDialogReducer(category, { type: "requestSucceeded", action: "setLocation" })).toBe(category)
  })

  it("closes on close", () => {
    expect(bulkDialogReducer(recheck, { type: "close" })).toBe(CLOSED_DIALOG)
  })

  it("confirms the pick of the dialog, with the direction of automatic management", () => {
    expect(dialogPick({ name: "reannounce" })).toEqual({ action: "reannounce" })
    expect(dialogPick({ name: "toggleAutoTMM", enable: true })).toEqual({ action: "toggleAutoTMM", enable: true })
  })
})

describe("bulkRequest", () => {
  const ticks: ActionTarget = { kind: "ticks", hashes: ["a", "b"], count: 2, previewTorrents: [] }

  it("sends the ticked hashes to the instance of the page", () => {
    expect(bulkRequest({ action: "pause" }, { instanceId: 7, actionTarget: ticks, totalSize: 0, excludedSize: 0, listFacts: LIST_FACTS })).toEqual({
      scope: 7,
      count: 2,
      body: { action: "pause", hashes: ["a", "b"], targets: undefined, selectAll: false, instanceIds: undefined },
    })
  })

  it("sends the instance subset of the page and the direction of a toggle", () => {
    const pairs: ActionTarget = { kind: "ticks", hashes: [], targets: [{ instanceId: 3, hash: "x" }, { instanceId: 4, hash: "y" }], count: 2, previewTorrents: [] }
    expect(bulkRequest({ action: "forceStart", enable: false }, { instanceId: 0, instanceIds: [3, 4], actionTarget: pairs, totalSize: 0, excludedSize: 0, listFacts: LIST_FACTS })).toEqual({
      scope: 0,
      count: 2,
      body: { action: "forceStart", enable: false, hashes: [], targets: [{ instanceId: 3, hash: "x" }, { instanceId: 4, hash: "y" }], selectAll: false, instanceIds: [3, 4] },
    })
  })

  it("sends the Select all scope with its Exclusions, and the instance subset of the target wins", () => {
    const filters = makeFilters({ categories: ["movies"] })
    const selectAll: ActionTarget = { kind: "selectAll", selectAll: { filters, search: "linux", instanceIds: [3], excludeTargets: [{ instanceId: 3, hash: "skip" }] }, count: 39, previewTorrents: [] }
    expect(bulkRequest({ action: "resume" }, { instanceId: 0, instanceIds: [3, 4], actionTarget: selectAll, totalSize: 0, excludedSize: 0, listFacts: LIST_FACTS })).toEqual({
      scope: 0,
      count: 39,
      body: { action: "resume", hashes: [], selectAll: true, filters, search: "linux", instanceIds: [3], excludeTargets: [{ instanceId: 3, hash: "skip" }] },
    })
  })
})

describe("deleteSize", () => {
  const facts = { instanceId: 7, totalSize: 0, excludedSize: 0, listFacts: LIST_FACTS }
  const loaded = [makeTorrent({ hash: "a", size: 100 }), makeTorrent({ hash: "b", size: 200 })]
  const selectAll: ActionTarget = { kind: "selectAll", selectAll: { filters: makeFilters(), search: "" }, count: 40, previewTorrents: loaded }

  it("gives the total of the filter minus the loaded Exclusions in Select all mode", () => {
    expect(deleteSize({ ...facts, actionTarget: selectAll, totalSize: 1000, excludedSize: 300 })).toBe(700)
  })

  it("gives the size of the ticked torrents", () => {
    const ticks: ActionTarget = { kind: "ticks", hashes: ["a", "b"], count: 2, previewTorrents: loaded }
    expect(deleteSize({ ...facts, actionTarget: ticks, totalSize: 1000 })).toBe(300)
  })

  it("gives the size of the loaded torrents of the Action target when the result is 0", () => {
    expect(deleteSize({ ...facts, actionTarget: selectAll })).toBe(300)
    expect(deleteSize({ ...facts, actionTarget: selectAll, totalSize: 300, excludedSize: 300 })).toBe(300)
  })
})

describe("dialogLists", () => {
  const facts = LIST_FACTS
  const metadata = { tags: ["instance-tag"], categories: { tv: { name: "tv", savePath: "/tv" } } }
  const listCategories = { movies: { name: "movies", savePath: "/movies" }, tv: { name: "tv", savePath: "/tv" } }

  it("shows each category and each tag of the list response in the Unified view", () => {
    const lists = dialogLists({ ...facts, isCrossInstance: true, categories: listCategories, tags: ["a", "b"] }, undefined, false)
    expect(lists).toEqual({ tags: ["a", "b"], categories: listCategories, isLoadingTags: false, isLoadingCategories: false })
  })

  it("falls back to the metadata of the instance when the cross-instance response has no lists", () => {
    const lists = dialogLists({ ...facts, isCrossInstance: true }, metadata, false)
    expect(lists).toMatchObject({ tags: ["instance-tag"], categories: metadata.categories })
  })

  it("shows the metadata of the instance on a single instance", () => {
    const lists = dialogLists({ ...facts, categories: listCategories, tags: ["a"] }, metadata, false)
    expect(lists).toMatchObject({ tags: ["instance-tag"], categories: metadata.categories })
  })

  it("shows the loading state while the metadata loads and the list is empty", () => {
    expect(dialogLists(facts, undefined, true)).toEqual({ tags: [], categories: {}, isLoadingTags: true, isLoadingCategories: true })
    const lists = dialogLists({ ...facts, isCrossInstance: true, categories: listCategories }, undefined, true)
    expect(lists).toMatchObject({ isLoadingTags: true, isLoadingCategories: false })
  })
})
