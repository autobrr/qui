// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { describe, expect, it } from "vitest"

import { resolveStreamFallbackStatus } from "@/lib/stream-status"

describe("resolveStreamFallbackStatus", () => {
  it("stays red on a cold start with no rows", () => {
    expect(
      resolveStreamFallbackStatus({ hasData: false, hasLastSuccessfulSync: false })
    ).toEqual({ tone: "error", variant: "offline", showStaleAge: false })
  })

  it("ignores a known sync time when there is no data to show", () => {
    // A last-good-sync without rows still means nothing is on screen, so do not
    // promise a stale-age line we cannot anchor to visible data.
    expect(
      resolveStreamFallbackStatus({ hasData: false, hasLastSuccessfulSync: true })
    ).toEqual({ tone: "error", variant: "offline", showStaleAge: false })
  })

  it("degrades to amber and shows the age when rows exist and a sync time is known", () => {
    expect(
      resolveStreamFallbackStatus({ hasData: true, hasLastSuccessfulSync: true })
    ).toEqual({ tone: "warning", variant: "degraded", showStaleAge: true })
  })

  it("degrades to amber but omits the age when the sync time is unknown", () => {
    expect(
      resolveStreamFallbackStatus({ hasData: true, hasLastSuccessfulSync: false })
    ).toEqual({ tone: "warning", variant: "degraded", showStaleAge: false })
  })
})
