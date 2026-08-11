/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { usePersistedCompactViewState } from "./usePersistedCompactViewState"

const STORAGE_KEY = "qui-torrent-view-mode"
const CARD_MODES = ["normal", "compact", "ultra-compact"] as const
const TABLE_MODES = ["normal", "dense", "compact"] as const

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("usePersistedCompactViewState", () => {
  it("keeps a stored mode a restricted consumer cannot render", () => {
    window.localStorage.setItem(STORAGE_KEY, "dense")

    // MobileFooterNav mounts on desktop too and has no "dense" option.
    const restricted = renderHook(() => usePersistedCompactViewState("compact", CARD_MODES))
    const table = renderHook(() => usePersistedCompactViewState("normal"))

    expect(restricted.result.current.viewMode).toBe("compact")
    expect(table.result.current.viewMode).toBe("dense")
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("dense")
  })

  it("broadcasts a user change to every consumer", () => {
    const table = renderHook(() => usePersistedCompactViewState("normal", TABLE_MODES))
    const restricted = renderHook(() => usePersistedCompactViewState("compact", CARD_MODES))

    act(() => table.result.current.setViewMode("dense"))

    expect(table.result.current.viewMode).toBe("dense")
    expect(restricted.result.current.viewMode).toBe("compact")
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("dense")

    act(() => restricted.result.current.setViewMode("ultra-compact"))

    expect(table.result.current.viewMode).toBe("normal")
    expect(restricted.result.current.viewMode).toBe("ultra-compact")
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("ultra-compact")
  })

  it("restores the stored mode after a narrow-then-wide resize", () => {
    window.localStorage.setItem(STORAGE_KEY, "dense")

    // FilterSidebar swaps allowedModes on the useIsMobile breakpoint.
    const { result, rerender } = renderHook(
      ({ mobile }: { mobile: boolean }) => usePersistedCompactViewState("compact", mobile ? CARD_MODES : undefined),
      { initialProps: { mobile: false } }
    )

    expect(result.current.viewMode).toBe("dense")

    rerender({ mobile: true })
    expect(result.current.viewMode).toBe("compact")
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("dense")

    rerender({ mobile: false })
    expect(result.current.viewMode).toBe("dense")
  })

  it("cycles within the allowed modes", () => {
    const { result } = renderHook(() => usePersistedCompactViewState("normal", TABLE_MODES))

    act(() => result.current.cycleViewMode())
    expect(result.current.viewMode).toBe("dense")

    act(() => result.current.cycleViewMode())
    expect(result.current.viewMode).toBe("compact")

    act(() => result.current.cycleViewMode())
    expect(result.current.viewMode).toBe("normal")
  })
})
