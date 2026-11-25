/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import type { TransferRow } from "@/lib/transfers-api"

import { canRetry, percentOf, timeLeftMs } from "./transfer-display"

function row(edit: Partial<TransferRow>): TransferRow {
  return {
    id: 1, kind: "transfer", way: "sftp", state: "waiting", startedBy: "user", ruleId: null, ruleName: "",
    sourceInstanceId: 1, targetInstanceId: 2, hash: "aaaa", name: "Show.S01.1080p-GRP", torrents: 1, size: 0,
    after: "removeWithFiles", leaveStopped: false, retryAt: null, failureClass: "", failureReason: "",
    createdAt: "2026-11-17T10:00:00Z", finishedAt: null, hints: [], progress: null,
    ...edit,
  }
}

describe("the Retry of a row", () => {
  it("waits for the end of the cleanup of a cancelled run", () => {
    expect(canRetry(row({ state: "cancelled", finishedAt: null }))).toBe(false)
    expect(canRetry(row({ state: "cancelled", finishedAt: "2026-11-17T11:00:00Z" }))).toBe(true)
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
