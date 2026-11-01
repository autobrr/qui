/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { decideSwipe } from "./useSwipeToClose"

const width = 400

describe("decideSwipe", () => {
  it("waits for 10 px of movement before it decides", () => {
    expect(decideSwipe("pending", { dx: 6, dy: 0, ms: 50, width }, false)).toBe("pending")
    expect(decideSwipe("pending", { dx: 10, dy: 0, ms: 50, width }, false)).toBe("follow")
  })

  it("follows a swipe to the right only when it is more than 1.5 times the vertical movement", () => {
    expect(decideSwipe("pending", { dx: 16, dy: 10, ms: 50, width }, false)).toBe("follow")
    expect(decideSwipe("pending", { dx: 15, dy: 10, ms: 50, width }, false)).toBe("ignore")
  })

  it("ignores a swipe to the left", () => {
    expect(decideSwipe("pending", { dx: -40, dy: 0, ms: 50, width }, false)).toBe("ignore")
  })

  it("keeps ignoring a gesture once it is a scroll", () => {
    expect(decideSwipe("ignore", { dx: 300, dy: 0, ms: 50, width }, true)).toBe("ignore")
  })

  it("closes on release past 30% of the width, and returns before it", () => {
    expect(decideSwipe("follow", { dx: 121, dy: 0, ms: 1000, width }, true)).toBe("close")
    expect(decideSwipe("follow", { dx: 120, dy: 0, ms: 1000, width }, true)).toBe("return")
  })

  it("closes a short flick faster than 0.5 px/ms", () => {
    expect(decideSwipe("follow", { dx: 60, dy: 0, ms: 100, width }, true)).toBe("close")
    expect(decideSwipe("follow", { dx: 60, dy: 0, ms: 120, width }, true)).toBe("return")
  })

  it("decides and releases a quick swipe in one step", () => {
    expect(decideSwipe("pending", { dx: 130, dy: 0, ms: 1000, width }, true)).toBe("close")
    expect(decideSwipe("pending", { dx: 4, dy: 0, ms: 20, width }, true)).toBe("ignore")
  })
})
