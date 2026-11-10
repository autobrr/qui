/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { libraryRunState, rssRunState, type LibraryRunInput, type RssRunInput } from "../run-state"

const t = (key: string, options?: Record<string, unknown>) => (options ? `${key} ${JSON.stringify(options)}` : key)

const readyRss: RssRunInput = {
  hasEnabledIndexers: true,
  hasTargets: true,
  running: false,
  cancelPending: false,
  startPending: false,
  cooldown: { active: false, minutes: 120, remaining: "" },
}

describe("rssRunState", () => {
  it("is ready when nothing blocks a run", () => {
    expect(rssRunState(readyRss, t)).toEqual({ kind: "ready" })
  })

  it.each([
    [{ hasEnabledIndexers: false }, "automation.requiresTorznabIndexer"],
    [{ hasTargets: false }, "toast.selectAtLeastOneInstance"],
    [{ cooldown: { active: true, minutes: 120, remaining: "5m" } }, "automation.runDisabledCooldown {\"minutes\":120,\"remaining\":\"5m\"}"],
    [{ startPending: true }, "run.starting"],
  ] satisfies [Partial<RssRunInput>, string][])("blocks with a reason for %j", (change, reason) => {
    expect(rssRunState({ ...readyRss, ...change }, t)).toEqual({ kind: "blocked", reason })
  })

  it("gives the first reason when two apply", () => {
    expect(rssRunState({ ...readyRss, hasEnabledIndexers: false, hasTargets: false }, t)).toEqual({ kind: "blocked", reason: "automation.requiresTorznabIndexer" })
    expect(rssRunState({ ...readyRss, hasTargets: false, cooldown: { active: true, minutes: 120, remaining: "5m" } }, t)).toEqual({ kind: "blocked", reason: "toast.selectAtLeastOneInstance" })
  })

  it("is running while a run is in progress, even when a reason applies", () => {
    expect(rssRunState({ ...readyRss, running: true, hasEnabledIndexers: false }, t)).toEqual({ kind: "running" })
  })

  it("is cancelling while a cancel request is pending", () => {
    expect(rssRunState({ ...readyRss, running: true, cancelPending: true }, t)).toEqual({ kind: "cancelling" })
  })
})

const readyLibrary: LibraryRunInput = {
  hasInstance: true,
  startPending: false,
  running: false,
  cancelPending: false,
  needsGazelle: false,
  hasSearchSource: true,
}

describe("libraryRunState", () => {
  it("is ready when nothing blocks a scan", () => {
    expect(libraryRunState(readyLibrary, t)).toEqual({ kind: "ready" })
  })

  it.each([
    [{ hasInstance: false }, "toast.selectInstanceToRun"],
    [{ startPending: true }, "run.starting"],
    [{ needsGazelle: true }, "toast.enableGazelleDescription"],
    [{ hasSearchSource: false }, "toast.configureTorznabOrGazelle"],
  ] satisfies [Partial<LibraryRunInput>, string][])("blocks with a reason for %j", (change, reason) => {
    expect(libraryRunState({ ...readyLibrary, ...change }, t)).toEqual({ kind: "blocked", reason })
  })

  it("gives the first reason when two apply", () => {
    expect(libraryRunState({ ...readyLibrary, hasInstance: false, needsGazelle: true }, t)).toEqual({ kind: "blocked", reason: "toast.selectInstanceToRun" })
    expect(libraryRunState({ ...readyLibrary, needsGazelle: true, hasSearchSource: false }, t)).toEqual({ kind: "blocked", reason: "toast.enableGazelleDescription" })
  })

  it("is running while a scan is in progress, even when a reason applies", () => {
    expect(libraryRunState({ ...readyLibrary, running: true, hasInstance: false }, t)).toEqual({ kind: "running" })
  })

  it("is cancelling while a cancel request is pending", () => {
    expect(libraryRunState({ ...readyLibrary, running: true, cancelPending: true }, t)).toEqual({ kind: "cancelling" })
  })
})
