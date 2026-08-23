/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  applyServerSettings,
  parseJsonBoolean,
  seedAndMarkReady,
  useClientSetting,
  writeRaw
} from "@/lib/client-settings"

const fetchMock = vi.fn()

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  fetchMock.mockReset()
  fetchMock.mockResolvedValue({ ok: true, status: 200 })
  vi.stubGlobal("fetch", fetchMock)
  vi.useFakeTimers()
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const boolSetting = { defaultValue: false, parse: parseJsonBoolean }

describe("useClientSetting", () => {
  it("returns the default when nothing is stored or the raw value is invalid", () => {
    const { result } = renderHook(() => useClientSetting("qui-test-bool", boolSetting))
    expect(result.current[0]).toBe(false)

    localStorage.setItem("qui-test-bool", "garbage")
    const second = renderHook(() => useClientSetting("qui-test-bool", boolSetting))
    expect(second.result.current[0]).toBe(false)
  })

  it("persists writes and keeps a second hook instance in sync", () => {
    const hookA = renderHook(() => useClientSetting("qui-test-bool", boolSetting))
    const hookB = renderHook(() => useClientSetting("qui-test-bool", boolSetting))

    act(() => hookA.result.current[1](true))

    expect(localStorage.getItem("qui-test-bool")).toBe("true")
    expect(hookB.result.current[0]).toBe(true)
  })

  it("supports functional updates", () => {
    const { result } = renderHook(() => useClientSetting("qui-test-bool", boolSetting))

    act(() => result.current[1]((prev) => !prev))

    expect(result.current[0]).toBe(true)
  })

  it("re-reads on a cross-tab storage event", () => {
    const { result } = renderHook(() => useClientSetting("qui-test-bool", boolSetting))

    localStorage.setItem("qui-test-bool", "true")
    act(() => {
      window.dispatchEvent(new Event("storage"))
    })

    expect(result.current[0]).toBe(true)
  })
})

describe("push queue", () => {
  it("sends nothing before the first successful GET opens the gate", () => {
    writeRaw("qui-test-bool", "true")
    vi.advanceTimersByTime(5_000)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("coalesces rapid writes into one debounced PUT with the last value per key", async () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    writeRaw("qui-test-other", "1")
    writeRaw("qui-test-bool", "false")

    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain("/client-settings")
    expect(init.method).toBe("PUT")
    expect(JSON.parse(init.body)).toEqual({ "qui-test-bool": "false", "qui-test-other": "1" })
  })

  it("skips the push when the value is unchanged", async () => {
    localStorage.setItem("qui-test-bool", "true")
    seedAndMarkReady({ "qui-test-bool": "true" })
    writeRaw("qui-test-bool", "true")

    await vi.runAllTimersAsync()

    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("requeues a failed batch for the next flush", async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 500 })
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.runAllTimersAsync()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // The next write flushes the failed key again alongside the new one.
    writeRaw("qui-test-other", "1")
    await vi.runAllTimersAsync()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({
      "qui-test-bool": "true",
      "qui-test-other": "1",
    })
  })
})

describe("applyServerSettings", () => {
  it("writes changed keys, reports them, and notifies live hooks", () => {
    const { result } = renderHook(() => useClientSetting("qui-test-bool", boolSetting))

    let changed: string[] = []
    act(() => {
      changed = applyServerSettings({ "qui-test-bool": "true", "qui-test-other": "1" })
    })

    expect(changed.sort()).toEqual(["qui-test-bool", "qui-test-other"])
    expect(result.current[0]).toBe(true)
    expect(localStorage.getItem("qui-test-other")).toBe("1")
  })

  it("never clobbers a pending local write (echo guard)", () => {
    writeRaw("qui-test-bool", "true")

    const changed = applyServerSettings({ "qui-test-bool": "false" })

    expect(changed).toEqual([])
    expect(localStorage.getItem("qui-test-bool")).toBe("true")
  })
})

describe("seedAndMarkReady", () => {
  it("pushes synced keys the server does not know, and only those", async () => {
    localStorage.setItem("qui-speed-units", "bits")
    localStorage.setItem("qui-incognito-mode", "true")
    localStorage.setItem("theme-cache", "{}") // not a synced key

    seedAndMarkReady({ "qui-incognito-mode": "true" })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-speed-units": "bits" })
  })
})
