/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ReactNode } from "react"
import { MobileScrollProvider, useMobileScroll } from "./MobileScrollContext"

const wrapper = ({ children }: { children: ReactNode }) => (
  <MobileScrollProvider>{children}</MobileScrollProvider>
)

let rafCallbacks: FrameRequestCallback[] = []

function scrollTo(container: HTMLElement, scrollTop: number) {
  container.scrollTop = scrollTop
  container.dispatchEvent(new Event("scroll"))
  const pending = rafCallbacks
  rafCallbacks = []
  pending.forEach(cb => cb(0))
}

describe("useMobileScroll", () => {
  beforeEach(() => {
    rafCallbacks = []
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      rafCallbacks.push(cb)
      return rafCallbacks.length
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it("hides on scroll down and shows again on scroll up", () => {
    const container = document.createElement("div")
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    expect(result.current.isFooterVisible).toBe(true)

    act(() => scrollTo(container, 100))
    expect(result.current.isFooterVisible).toBe(false)

    act(() => scrollTo(container, 50))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("ignores movement below the threshold", () => {
    const container = document.createElement("div")
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 5))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("resets to visible when the scroll container unregisters", () => {
    const container = document.createElement("div")
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 100))
    expect(result.current.isFooterVisible).toBe(false)

    act(() => result.current.setScrollContainer(null))
    expect(result.current.isFooterVisible).toBe(true)
  })
})
