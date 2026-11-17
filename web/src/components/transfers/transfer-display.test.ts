/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import type { TransferRow } from "@/lib/transfers-api"

import { canCancel, canRemove, canRetry, chipOf, percentOf, stepKey, timeLeftMs, waitsForRetry } from "./transfer-display"

function row(edit: Partial<TransferRow>): TransferRow {
  return {
    id: 1, kind: "transfer", way: "sftp", state: "waiting", startedBy: "user", ruleId: null, ruleName: "",
    sourceInstanceId: 1, targetInstanceId: 2, hash: "aaaa", name: "Show.S01.1080p-GRP", torrents: 1,
    after: "removeWithFiles", leaveStopped: false, retryAt: null, failureClass: "", failureReason: "",
    createdAt: "2026-11-17T10:00:00Z", finishedAt: null, hints: [], progress: null,
    ...edit,
  }
}

describe("the chips", () => {
  it("put each state behind the chip of the backend filter", () => {
    expect(chipOf("copying")).toBe("active")
    expect(chipOf("checking")).toBe("active")
    expect(chipOf("finishing")).toBe("active")
    expect(chipOf("waiting")).toBe("waiting")
    expect(chipOf("done")).toBe("finished")
    expect(chipOf("cancelled")).toBe("finished")
    expect(chipOf("failed")).toBe("failed")
  })
})

describe("the actions of a row", () => {
  it("cancel a run that has not ended, and remove only a finished one", () => {
    const running = row({ state: "copying" })
    expect(canCancel(running)).toBe(true)
    expect(canRemove(running)).toBe(false)

    const done = row({ state: "done", finishedAt: "2026-11-17T11:00:00Z" })
    expect(canCancel(done)).toBe(false)
    expect(canRemove(done)).toBe(true)
  })

  it("retry a failed or cancelled run, never a done one", () => {
    expect(canRetry(row({ state: "failed", finishedAt: "2026-11-17T11:00:00Z" }))).toBe(true)
    expect(canRetry(row({ state: "cancelled", finishedAt: "2026-11-17T11:00:00Z" }))).toBe(true)
    expect(canRetry(row({ state: "done", finishedAt: "2026-11-17T11:00:00Z" }))).toBe(false)
    expect(canRetry(row({ state: "copying" }))).toBe(false)
  })

  it("show the retry time only for a failure that waits for its retry", () => {
    expect(waitsForRetry(row({ state: "failed", retryAt: "2026-11-17T10:05:00Z" }))).toBe(true)
    expect(waitsForRetry(row({ state: "failed", retryAt: null, finishedAt: "2026-11-17T11:00:00Z" }))).toBe(false)
    expect(waitsForRetry(row({ state: "waiting", retryAt: null }))).toBe(false)
  })
})

describe("the progress of a row", () => {
  it("rounds the share down and keeps it at most 100", () => {
    expect(percentOf({ done: 999, total: 1000, speed: 0 })).toBe(99)
    expect(percentOf({ done: 1000, total: 1000, speed: 0 })).toBe(100)
    expect(percentOf({ done: 5, total: 0, speed: 0 })).toBe(0)
  })

  it("knows the time left only with a speed", () => {
    expect(timeLeftMs({ done: 400, total: 1000, speed: 200 })).toBe(3000)
    expect(timeLeftMs({ done: 400, total: 1000, speed: 0 })).toBeNull()
  })
})

describe("the steps of the Details panel", () => {
  it("name the key of each step that the backend records, and none of a step it does not know", () => {
    expect(stepKey("pass by BitTorrent")).toBe("steps.passByBitTorrent")
    expect(stepKey("check on the target")).toBe("steps.check")
    expect(stepKey("remove the placed files")).toBe("steps.removePlaced")
    expect(stepKey("copy the pieces that failed the check")).toBe("steps.copyFailedPieces")
    expect(stepKey("cancelled")).toBe("states.cancelled")
    expect(stepKey("teleport")).toBeNull()
    expect(stepKey("toString")).toBeNull()
  })
})
