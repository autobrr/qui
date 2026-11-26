/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ALL_LOG_LEVELS, usePersistedLogLevels } from "@/hooks/usePersistedLogLevels"
import { _resetClientSettingsForTests } from "@/lib/client-settings"

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("usePersistedLogLevels", () => {
  it.each([
    ["nothing stored", null, ALL_LOG_LEVELS],
    ["unparseable value", "garbage", ALL_LOG_LEVELS],
    ["non-array value", "{\"warn\":true}", ALL_LOG_LEVELS],
    ["stored subset", "[\"warn\",\"error\"]", ["warn", "error"]],
    ["unknown levels dropped", "[\"warn\",\"fatal\",1]", ["warn"]],
    ["empty selection kept", "[]", []],
  ])("%s", (_name, raw, expected) => {
    if (raw !== null) localStorage.setItem("qui-log-levels", raw)
    const { result } = renderHook(() => usePersistedLogLevels())
    expect([...result.current[0]]).toEqual(expected)
  })

  it("persists a new selection", () => {
    const { result } = renderHook(() => usePersistedLogLevels())
    act(() => result.current[1](new Set(["warn", "error"])))

    expect(localStorage.getItem("qui-log-levels")).toBe("[\"warn\",\"error\"]")
    const second = renderHook(() => usePersistedLogLevels())
    expect([...second.result.current[0]]).toEqual(["warn", "error"])
  })
})
