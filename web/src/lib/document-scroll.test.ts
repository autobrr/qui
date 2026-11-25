/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { keepDocumentAtTop } from "./document-scroll"

function fakeWindow() {
  const target = new EventTarget()
  const win = {
    scrollX: 0,
    scrollY: 0,
    scrollTo(x: number, y: number) {
      win.scrollX = x
      win.scrollY = y
    },
    addEventListener: target.addEventListener.bind(target),
    fire(type: "resize" | "scroll", scrollY: number) {
      win.scrollY = scrollY
      target.dispatchEvent(new Event(type))
    },
  }
  return win
}

describe("keepDocumentAtTop", () => {
  it("scrolls the document back to the top after a rotation back to portrait", () => {
    const win = fakeWindow()
    keepDocumentAtTop(win as unknown as Window)

    win.fire("resize", 0)
    win.fire("resize", 62)

    expect(win.scrollY).toBe(0)
  })

  it("leaves a scroll alone when the window keeps its size, as when iOS reveals a focused field", () => {
    const win = fakeWindow()
    keepDocumentAtTop(win as unknown as Window)

    win.fire("scroll", 120)

    expect(win.scrollY).toBe(120)
  })
})
