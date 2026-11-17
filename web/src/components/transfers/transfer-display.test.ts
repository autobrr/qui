/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { percentOf, timeLeftMs } from "./transfer-display"

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
