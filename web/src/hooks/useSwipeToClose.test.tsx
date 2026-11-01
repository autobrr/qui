/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { decideSwipe, useSwipeToClose } from "./useSwipeToClose"

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

function View({ onClose }: { onClose: () => void }) {
  return <div data-testid="view" {...useSwipeToClose(onClose)} />
}

const finger = (clientX: number, clientY = 0) => ({ clientX, clientY })

// A view that follows a finger 100 px to the right
function startSwipe() {
  const onClose = vi.fn()
  render(<View onClose={onClose} />)
  const view = screen.getByTestId("view")
  fireEvent.touchStart(view, { touches: [finger(0)] })
  fireEvent.touchMove(view, { touches: [finger(100)] })
  expect(view.style.transform).toBe("translateX(100px)")
  return { view, onClose }
}

describe("useSwipeToClose", () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  // regression: #3703
  it("returns the view to its place when the browser cancels the touch", () => {
    vi.useFakeTimers()
    const { view, onClose } = startSwipe()
    fireEvent.touchCancel(view, { touches: [] })
    vi.runAllTimers()
    expect(onClose).not.toHaveBeenCalled()
    expect(view.style.transform).toBe("")
  })

  // regression: #3703
  it("returns the view to its place when a second finger touches it", () => {
    vi.useFakeTimers()
    const { view, onClose } = startSwipe()
    fireEvent.touchStart(view, { touches: [finger(100), finger(200)] })
    fireEvent.touchEnd(view, { touches: [] })
    vi.runAllTimers()
    expect(onClose).not.toHaveBeenCalled()
    expect(view.style.transform).toBe("")
  })
})
