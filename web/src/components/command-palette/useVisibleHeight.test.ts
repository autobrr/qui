/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useVisibleHeight } from "./useVisibleHeight"

afterEach(() => {
  // The hook stops its listener on the viewport when it unmounts, so it unmounts while the viewport is there.
  cleanup()
  vi.unstubAllGlobals()
})

describe("useVisibleHeight", () => {
  it("follows the height that an on-screen keyboard leaves free", () => {
    const viewport = Object.assign(new EventTarget(), { height: 844 })
    vi.stubGlobal("visualViewport", viewport)
    const { result } = renderHook(() => useVisibleHeight())
    expect(result.current).toBe(844)

    viewport.height = 508
    act(() => {
      viewport.dispatchEvent(new Event("resize"))
    })

    expect(result.current).toBe(508)
  })

  it("gives no height in a browser that does not tell it", () => {
    vi.stubGlobal("visualViewport", undefined)

    expect(renderHook(() => useVisibleHeight()).result.current).toBeNull()
  })
})
