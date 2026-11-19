/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { swipeEndsOpen, swipeOffset } from "./useSwipeActions"

const width = 140

describe("swipeOffset", () => {
  it("follows the finger between the closed place and the width of the actions", () => {
    expect(swipeOffset(0, -60, width)).toBe(-60)
    expect(swipeOffset(-width, 50, width)).toBe(-90)
  })

  it("stops at the closed place", () => {
    expect(swipeOffset(0, 40, width)).toBe(0)
    expect(swipeOffset(-width, 200, width)).toBe(0)
  })

  it("moves at a fifth of the finger past the actions", () => {
    expect(swipeOffset(0, -240, width)).toBe(-160)
  })
})

describe("swipeEndsOpen", () => {
  it("opens past half of the width, and closes before it", () => {
    expect(swipeEndsOpen(-71, 0, width)).toBe(true)
    expect(swipeEndsOpen(-70, 0, width)).toBe(false)
  })

  it("follows the direction of a flick faster than 0.3 px/ms", () => {
    expect(swipeEndsOpen(-20, -0.31, width)).toBe(true)
    expect(swipeEndsOpen(-20, -0.3, width)).toBe(false)
    expect(swipeEndsOpen(-120, 0.31, width)).toBe(false)
  })
})
