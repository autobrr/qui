/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { bulkDialogReducer, bulkRequest, CLOSED_DIALOG, dialogPick, resolvePick, type BulkDialog } from "../bulk-action-state"
import type { ActionTarget } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"

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
    expect(bulkRequest({ action: "pause" }, { instanceId: 7, actionTarget: ticks, selectedTotalSize: 0 })).toEqual({
      scope: 7,
      count: 2,
      body: { action: "pause", hashes: ["a", "b"], targets: undefined, selectAll: false, instanceIds: undefined },
    })
  })

  it("sends the instance subset of the page and the direction of a toggle", () => {
    const pairs: ActionTarget = { kind: "ticks", hashes: [], targets: [{ instanceId: 3, hash: "x" }, { instanceId: 4, hash: "y" }], count: 2, previewTorrents: [] }
    expect(bulkRequest({ action: "forceStart", enable: false }, { instanceId: 0, instanceIds: [3, 4], actionTarget: pairs, selectedTotalSize: 0 })).toEqual({
      scope: 0,
      count: 2,
      body: { action: "forceStart", enable: false, hashes: [], targets: [{ instanceId: 3, hash: "x" }, { instanceId: 4, hash: "y" }], selectAll: false, instanceIds: [3, 4] },
    })
  })

  it("sends the Select all scope with its Exclusions, and the instance subset of the target wins", () => {
    const filters = makeFilters({ categories: ["movies"] })
    const selectAll: ActionTarget = { kind: "selectAll", selectAll: { filters, search: "linux", instanceIds: [3], excludeTargets: [{ instanceId: 3, hash: "skip" }] }, count: 39, previewTorrents: [] }
    expect(bulkRequest({ action: "resume" }, { instanceId: 0, instanceIds: [3, 4], actionTarget: selectAll, selectedTotalSize: 0 })).toEqual({
      scope: 0,
      count: 39,
      body: { action: "resume", hashes: [], selectAll: true, filters, search: "linux", instanceIds: [3], excludeTargets: [{ instanceId: 3, hash: "skip" }] },
    })
  })
})
