/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { usePersistedCompactViewState } from "./usePersistedCompactViewState"

const MOBILE_STORAGE_KEY = "qui-torrent-mobile-view-mode"
const DESKTOP_STORAGE_KEY = "qui-torrent-desktop-view-mode"

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("usePersistedCompactViewState", () => {
  it("keeps mobile and desktop choices independent", () => {
    window.localStorage.setItem(MOBILE_STORAGE_KEY, "compact")
    window.localStorage.setItem(DESKTOP_STORAGE_KEY, "dense")

    const mobile = renderHook(() => usePersistedCompactViewState("mobile"))
    const desktop = renderHook(() => usePersistedCompactViewState("desktop"))

    expect(mobile.result.current.viewMode).toBe("compact")
    expect(desktop.result.current.viewMode).toBe("dense")

    act(() => mobile.result.current.setViewMode("normal"))

    expect(mobile.result.current.viewMode).toBe("normal")
    expect(desktop.result.current.viewMode).toBe("dense")
    expect(window.localStorage.getItem(MOBILE_STORAGE_KEY)).toBe("normal")
    expect(window.localStorage.getItem(DESKTOP_STORAGE_KEY)).toBe("dense")
  })

  it("restores each saved choice when the active layout changes", () => {
    window.localStorage.setItem(MOBILE_STORAGE_KEY, "ultra-compact")
    window.localStorage.setItem(DESKTOP_STORAGE_KEY, "dense")

    const { result, rerender } = renderHook(
      ({ layout }: { layout: "mobile" | "desktop" }) => usePersistedCompactViewState(layout),
      { initialProps: { layout: "mobile" as "mobile" | "desktop" } }
    )

    expect(result.current.viewMode).toBe("ultra-compact")

    rerender({ layout: "desktop" })
    expect(result.current.viewMode).toBe("dense")

    rerender({ layout: "mobile" })
    expect(result.current.viewMode).toBe("ultra-compact")
  })

  it("cycles only through modes supported by the active layout", () => {
    const mobile = renderHook(() => usePersistedCompactViewState("mobile"))
    const desktop = renderHook(() => usePersistedCompactViewState("desktop"))

    act(() => mobile.result.current.cycleViewMode())
    expect(mobile.result.current.viewMode).toBe("ultra-compact")
    act(() => mobile.result.current.cycleViewMode())
    expect(mobile.result.current.viewMode).toBe("normal")

    act(() => desktop.result.current.cycleViewMode())
    expect(desktop.result.current.viewMode).toBe("dense")
    act(() => desktop.result.current.cycleViewMode())
    expect(desktop.result.current.viewMode).toBe("compact")
  })
})
