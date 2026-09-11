/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { usePersistedStretchMode } from "./usePersistedStretchMode"

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("usePersistedStretchMode", () => {
  it("defaults to stretch and persists the toggle per instance", () => {
    const { result } = renderHook(() => usePersistedStretchMode(1))
    expect(result.current[0]).toBe(true)

    act(() => result.current[1]())

    expect(result.current[0]).toBe(false)
    expect(window.localStorage.getItem("qui-stretch-name-column:1")).toBe("false")
  })

  it("restores the saved choice and keeps instances independent", () => {
    window.localStorage.setItem("qui-stretch-name-column:1", "false")

    const first = renderHook(() => usePersistedStretchMode(1))
    const second = renderHook(() => usePersistedStretchMode(2))

    expect(first.result.current[0]).toBe(false)
    expect(second.result.current[0]).toBe(true)
  })

  it("uses the shared key when no instance is selected", () => {
    const { result } = renderHook(() => usePersistedStretchMode(0))

    act(() => result.current[1]())

    expect(window.localStorage.getItem("qui-stretch-name-column")).toBe("false")
  })
})
