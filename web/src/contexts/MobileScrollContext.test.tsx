/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, render, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useRef } from "react"
import type { ReactNode } from "react"
import { MobileScrollProvider, useMobileScroll, useRegisterMobileScrollContainer } from "./MobileScrollContext"

const wrapper = ({ children }: { children: ReactNode }) => (
  <MobileScrollProvider>{children}</MobileScrollProvider>
)

let rafQueue = new Map<number, FrameRequestCallback>()
let rafId = 0

function flushRaf() {
  const pending = [...rafQueue.values()]
  rafQueue.clear()
  pending.forEach(cb => cb(0))
}

// jsdom does no layout, so give the container a list that is taller than the screen
function makeContainer(size = { scrollHeight: 1000, clientHeight: 400 }) {
  const el = document.createElement("div")
  Object.defineProperty(el, "scrollHeight", { get: () => size.scrollHeight })
  Object.defineProperty(el, "clientHeight", { get: () => size.clientHeight })
  return el
}

function scrollTo(container: HTMLElement, scrollTop: number) {
  container.scrollTop = scrollTop
  container.dispatchEvent(new Event("scroll"))
  flushRaf()
}

describe("useMobileScroll", () => {
  beforeEach(() => {
    rafQueue = new Map()
    rafId = 0
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      rafQueue.set(++rafId, cb)
      return rafId
    })
    vi.stubGlobal("cancelAnimationFrame", (id: number) => {
      rafQueue.delete(id)
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    cleanup()
  })

  it("hides on scroll down and shows again on scroll up", () => {
    const container = makeContainer()
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    expect(result.current.isFooterVisible).toBe(true)

    act(() => scrollTo(container, 100))
    expect(result.current.isFooterVisible).toBe(false)

    act(() => scrollTo(container, 50))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("keeps the footer hidden when the list shrinks under a container scrolled to the bottom", () => {
    const size = { scrollHeight: 1000, clientHeight: 400 }
    const container = makeContainer(size)
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 600))
    expect(result.current.isFooterVisible).toBe(false)

    // The footer padding goes away, so the browser clamps scrollTop to the new bottom
    size.scrollHeight = 808
    act(() => scrollTo(container, 408))
    expect(result.current.isFooterVisible).toBe(false)

    // A real scroll up away from the bottom still shows the footer
    act(() => scrollTo(container, 300))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("shows the footer when the list shrinks until it no longer scrolls", () => {
    const size = { scrollHeight: 1000, clientHeight: 400 }
    const container = makeContainer(size)
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 600))
    expect(result.current.isFooterVisible).toBe(false)

    size.scrollHeight = 400
    act(() => scrollTo(container, 0))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("ignores movement below the threshold", () => {
    const container = document.createElement("div")
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 5))
    expect(result.current.isFooterVisible).toBe(true)
  })

  it("keeps the new list registered when the old list unmounts after it", () => {
    const oldEl = document.createElement("div")
    const newEl = document.createElement("div")
    let footerVisible = true

    function List({ el }: { el: HTMLElement }) {
      const ref = useRef(el)
      useRegisterMobileScrollContainer(ref)
      return null
    }
    function Probe() {
      footerVisible = useMobileScroll().isFooterVisible
      return null
    }
    function Harness({ phase }: { phase: number }) {
      return (
        <MobileScrollProvider>
          {phase <= 1 && <List el={oldEl} />}
          {phase >= 1 && <List el={newEl} />}
          <Probe />
        </MobileScrollProvider>
      )
    }

    // Old list mounts, new list mounts beside it, then the old one unmounts —
    // the route-transition interleaving where cleanup runs after the takeover.
    const { rerender } = render(<Harness phase={0} />)
    rerender(<Harness phase={1} />)
    rerender(<Harness phase={2} />)

    // The new list must still drive footer visibility
    act(() => scrollTo(newEl, 100))
    expect(footerVisible).toBe(false)
  })

  it("cancels a queued frame when the container unregisters", () => {
    const container = document.createElement("div")
    const { result } = renderHook(() => useMobileScroll(), { wrapper })

    act(() => result.current.setScrollContainer(container))
    act(() => scrollTo(container, 100))
    expect(result.current.isFooterVisible).toBe(false)

    // Queue a scroll-down frame but unregister before it runs
    act(() => {
      container.scrollTop = 200
      container.dispatchEvent(new Event("scroll"))
    })
    act(() => result.current.setScrollContainer(null))
    act(() => flushRaf())
    expect(result.current.isFooterVisible).toBe(true)
  })
})
