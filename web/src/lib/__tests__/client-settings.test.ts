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

  // REGRESSION: only visibilitychange flushed the queue, and a same-tab
  // reload does not fire it, so a write inside the debounce window was lost
  // and the stale server snapshot reverted it at next boot.
  it("flushes pending writes on pagehide without waiting for the debounce", () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    expect(fetchMock).not.toHaveBeenCalled()

    window.dispatchEvent(new Event("pagehide"))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })
  })

  // REGRESSION: pending was cleared before the PUT resolved, so a server
  // apply during the in-flight window clobbered the newer value.
  it("keeps in-flight keys guarded against a concurrent server apply", async () => {
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // PUT is in flight; a stale snapshot must not overwrite the newer value.
    const changed = applyServerSettings({ "qui-test-bool": "false" })
    expect(changed).toEqual([])
    expect(localStorage.getItem("qui-test-bool")).toBe("true")

    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()
    expect(localStorage.getItem("qui-test-bool")).toBe("true")
  })

  it("flushes a value written while a PUT for the same key is in flight", async () => {
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Newer write lands mid-flight; it must survive the first PUT's cleanup.
    writeRaw("qui-test-bool", "false")
    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ "qui-test-bool": "false" })
  })

  // REGRESSION: a flush whose timer fired during an in-flight PUT was
  // swallowed; if that PUT then failed, the newer write stalled until the
  // next write or tab-hide.
  it("replays a flush swallowed during a failed in-flight PUT", async () => {
    let rejectPut: (reason: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectPut = reject }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // New write mid-flight; its timer fires while the PUT is still pending.
    writeRaw("qui-test-other", "1")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    rejectPut(new Error("network down"))
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({
      "qui-test-bool": "true",
      "qui-test-other": "1",
    })
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

describe("outbox persistence", () => {
  it("replays a write whose unload flush never reached the server", async () => {
    // A pagehide keepalive PUT can die in flight (Chrome drops keepalive
    // fetches from service-worker-controlled pages on unload), so an instant
    // reload after a write must recover from the persisted outbox.
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")

    // Page dies before the debounced flush is acked; next boot, stale server.
    _resetClientSettingsForTests()
    fetchMock.mockClear()

    applyServerSettings({ "qui-test-bool": "false" })
    expect(localStorage.getItem("qui-test-bool")).toBe("true")

    seedAndMarkReady({ "qui-test-bool": "false" })
    await vi.runAllTimersAsync()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })
  })

  it("an acked write leaves no outbox entry to replay", async () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.runAllTimersAsync()

    _resetClientSettingsForTests()

    const changed = applyServerSettings({ "qui-test-bool": "false" })
    expect(changed).toEqual(["qui-test-bool"])
    expect(localStorage.getItem("qui-test-bool")).toBe("false")
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
