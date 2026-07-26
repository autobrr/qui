/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { isNeverCompletedTimestamp } from "@/lib/dateTimeUtils"

describe("isNeverCompletedTimestamp", () => {
  it("rejects qBittorrent never-completed sentinels", () => {
    expect(isNeverCompletedTimestamp(0)).toBe(true)
    expect(isNeverCompletedTimestamp(-1)).toBe(true)
    expect(isNeverCompletedTimestamp(28800)).toBe(true) // qbit 4.x west of UTC
    expect(isNeverCompletedTimestamp(86400)).toBe(true) // boundary
    expect(isNeverCompletedTimestamp(4294967295)).toBe(true) // qbit 4.1 uint32(-1)
    expect(isNeverCompletedTimestamp(undefined)).toBe(true)
  })

  it("accepts real completion timestamps", () => {
    expect(isNeverCompletedTimestamp(1700000123)).toBe(false)
    expect(isNeverCompletedTimestamp(86401)).toBe(false)
  })
})
