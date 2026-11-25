/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  createDisplayProfile,
  deleteDisplayProfile,
  getDisplayProfiles,
  migrateLegacyLocalValue,
  onActiveDisplayProfileDeleted,
  pickDisplayProfile,
  applyServerSettings,
  parseJsonBoolean,
  readClientSetting,
  recordColumnSetupInstance,
  renameDisplayProfile,
  seedAndMarkReady,
  setDisplayProfileColor,
  setDisplayProfileIcon,
  setDisplayProfileSameColumns,
  setDisplayProfileOwnSortAndFilters,
  useClientSetting,
  useDisplayProfiles,
  useDisplayProfileSetting,
  useLastColumnSetupInstance,
  writeClientSetting,
  writeRaw
} from "@/lib/client-settings"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"

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

describe("writeClientSetting", () => {
  it("updates a live hook and readClientSetting, and pushes the value like the hook setter", async () => {
    seedAndMarkReady({})
    const { result } = renderHook(() => useClientSetting("qui-test-bool", boolSetting))

    act(() => writeClientSetting("qui-test-bool", true))

    expect(result.current[0]).toBe(true)
    expect(readClientSetting("qui-test-bool", boolSetting)).toBe(true)
    await vi.runAllTimersAsync()
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })
  })

  it("encodes the value with a custom serializer", () => {
    writeClientSetting("qui-test-mode", "dense", (mode) => mode)

    expect(readClientSetting("qui-test-mode", { defaultValue: "", parse: (raw) => raw })).toBe("dense")
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

  it("flushes pending writes on pagehide without waiting for the debounce", () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    expect(fetchMock).not.toHaveBeenCalled()

    window.dispatchEvent(new Event("pagehide"))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })
  })

  it("keeps in-flight keys guarded against a concurrent server apply", async () => {
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

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

    writeRaw("qui-test-bool", "false")
    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ "qui-test-bool": "false" })
  })

  it("replays a newer cross-tab value after an older PUT finishes", async () => {
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })

    localStorage.setItem("qui-test-bool", "false")
    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ "qui-test-bool": "false" })
  })

  it("replays a cross-tab edit made after a server copy", async () => {
    vi.resetModules()
    const tabB = await import("@/lib/client-settings")
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    applyServerSettings({ "qui-test-bool": "false" })
    seedAndMarkReady({ "qui-test-bool": "false" })
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)

    tabB.writeRaw("qui-test-bool", "false")
    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ "qui-test-bool": "false" })
  })

  it.each([1, 3])("does not push a server copy back after two quick writes, with %i other tabs", async (otherTabCount) => {
    const otherTabs: (typeof import("@/lib/client-settings"))[] = []
    for (let i = 0; i < otherTabCount; i++) {
      vi.resetModules()
      otherTabs.push(await import("@/lib/client-settings"))
    }
    const key = "qui-sidebar-icon-only"
    const server: Record<string, string> = {}
    fetchMock.mockImplementation((_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      const snapshot = { ...server }
      setTimeout(() => applyServerSettings(snapshot), 50)
      otherTabs.forEach((tab, i) => setTimeout(() => tab.applyServerSettings(snapshot), 50 + 20 * i))
      return new Promise((resolve) => setTimeout(() => resolve({ ok: true, status: 200 }), 10))
    })
    seedAndMarkReady({})
    for (const tab of otherTabs) tab.seedAndMarkReady({})

    writeRaw(key, "true")
    await vi.advanceTimersByTimeAsync(805)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    writeRaw(key, "false")
    await vi.advanceTimersByTimeAsync(20_000)

    expect(fetchMock.mock.calls.length).toBeLessThanOrEqual(3)
    expect(server).toEqual({ [key]: "false" })
    expect(localStorage.getItem(key)).toBe("false")

    otherTabs[0]._resetClientSettingsForTests()
    otherTabs[0].seedAndMarkReady(server)
    await vi.runAllTimersAsync()
    for (const [, init] of fetchMock.mock.calls) {
      expect(Object.keys(JSON.parse(init.body))).toEqual([key])
    }
  })

  it("replays a flush swallowed during a failed in-flight PUT", async () => {
    let rejectPut: (reason: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectPut = reject }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)

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

    writeRaw("qui-test-other", "1")
    await vi.runAllTimersAsync()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({
      "qui-test-bool": "true",
      "qui-test-other": "1",
    })
  })
})

describe("large batches", () => {
  const MAX_BODY_BYTES = 64 * 1024

  function storeLargeLocalSetup(): Record<string, string> {
    const values: Record<string, string> = {}
    for (let i = 0; i < 50; i++) {
      values[`qui-column-visibility:${i}`] = JSON.stringify({ name: true, note: "x".repeat(2_000) })
    }
    for (const [key, raw] of Object.entries(values)) localStorage.setItem(key, raw)
    return values
  }

  function fakeServer(): Record<string, string> {
    const server: Record<string, string> = {}
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      return { ok: true, status: 200 }
    })
    return server
  }

  function bodySizes(): number[] {
    return fetchMock.mock.calls.map(([, init]) => new TextEncoder().encode(String(init.body)).length)
  }

  it("syncs every key when the first batch is over 64 KiB", async () => {
    const values = storeLargeLocalSetup()
    const server = fakeServer()

    seedAndMarkReady({})
    await vi.runAllTimersAsync()

    expect(fetchMock.mock.calls.length).toBeGreaterThan(1)
    for (const size of bodySizes()) expect(size).toBeLessThanOrEqual(MAX_BODY_BYTES)
    expect(server).toEqual(values)
  })

  it("replays the keys of a failed PUT after a split batch, on the next page load", async () => {
    const stale: Record<string, string> = {}
    for (let i = 0; i < 50; i++) stale[`qui-column-visibility:${i}`] = "{}"
    const server = fakeServer()
    Object.assign(server, stale)
    applyServerSettings(stale)
    seedAndMarkReady(stale)
    const edited: Record<string, string> = {}
    for (let i = 0; i < 50; i++) {
      edited[`qui-column-visibility:${i}`] = JSON.stringify({ name: true, note: "x".repeat(2_000) })
      writeRaw(`qui-column-visibility:${i}`, edited[`qui-column-visibility:${i}`])
    }
    const deliver = fetchMock.getMockImplementation()!
    fetchMock.mockImplementationOnce(deliver).mockResolvedValueOnce({ ok: false, status: 500 })
    await vi.runAllTimersAsync()
    expect(server).not.toEqual(edited)

    _resetClientSettingsForTests()
    applyServerSettings({ ...server })
    seedAndMarkReady({ ...server })
    await vi.runAllTimersAsync()

    expect(server).toEqual(edited)
  })

  it("sends a debounced save without keepalive", async () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][1].keepalive).toBeFalsy()
  })

  it("sends a debounced save that is still in flight at page hide again, with keepalive and the waiting saves", async () => {
    const values = storeLargeLocalSetup()
    const server = fakeServer()
    const deliver = fetchMock.getMockImplementation()!
    fetchMock.mockImplementationOnce(() => new Promise(() => {}))
    seedAndMarkReady({})
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    writeRaw("qui-test-bool", "true")
    fetchMock.mockImplementation(deliver)

    window.dispatchEvent(new Event("pagehide"))
    await vi.runAllTimersAsync()

    const resent = fetchMock.mock.calls.slice(1)
    expect(resent.length).toBeGreaterThan(1)
    for (const [, init] of resent) expect(init.keepalive).toBe(true)
    for (const size of bodySizes().slice(1)) expect(size).toBeLessThanOrEqual(MAX_BODY_BYTES)
    expect(server).toEqual({ ...values, "qui-test-bool": "true" })
  })

  it("sends the newer value again when the page-hide PUT lands after a newer save", async () => {
    const server: Record<string, string> = {}
    const inFlight: (() => void)[] = []
    fetchMock.mockImplementation((_url: string, init: RequestInit) => new Promise((resolve) => {
      inFlight.push(() => {
        Object.assign(server, JSON.parse(String(init.body)))
        resolve({ ok: true, status: 200 })
      })
    }))
    const land = async (index: number) => {
      inFlight[index]()
      await vi.advanceTimersByTimeAsync(0)
    }
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "1")
    await vi.advanceTimersByTimeAsync(1_000)
    window.dispatchEvent(new Event("pagehide"))
    expect(inFlight).toHaveLength(2)

    await land(0)
    writeRaw("qui-test-bool", "2")
    await vi.advanceTimersByTimeAsync(1_000)
    await land(2)
    await land(1)
    await vi.advanceTimersByTimeAsync(1_000)
    for (let i = 3; i < inFlight.length; i++) await land(i)

    expect(server["qui-test-bool"]).toBe("2")
  })

  it("keeps the newer value when the page-hide PUT lands before an older PUT and a snapshot of it", async () => {
    const server: Record<string, string> = {}
    const inFlight: (() => void)[] = []
    fetchMock.mockImplementation((_url: string, init: RequestInit) => new Promise((resolve) => {
      inFlight.push(() => {
        Object.assign(server, JSON.parse(String(init.body)))
        resolve({ ok: true, status: 200 })
      })
    }))
    const land = async (index: number) => {
      inFlight[index]()
      await vi.advanceTimersByTimeAsync(0)
    }
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "1")
    await vi.advanceTimersByTimeAsync(1_000)
    writeRaw("qui-test-bool", "2")
    window.dispatchEvent(new Event("pagehide"))
    expect(inFlight).toHaveLength(2)

    await land(1)
    applyServerSettings({ "qui-test-bool": "1" })
    await land(0)
    await vi.advanceTimersByTimeAsync(1_000)
    for (let i = 2; i < inFlight.length; i++) await land(i)

    expect(localStorage.getItem("qui-test-bool")).toBe("2")
    expect(server["qui-test-bool"]).toBe("2")
  })

  it("sends the newer value again when the page-hide PUT commits after a newer PUT but answers first", async () => {
    const server: Record<string, string> = {}
    const puts: { commit: () => void; answer: () => void }[] = []
    fetchMock.mockImplementation((_url: string, init: RequestInit) => new Promise((resolve) => {
      puts.push({
        commit: () => Object.assign(server, JSON.parse(String(init.body))),
        answer: () => resolve({ ok: true, status: 200 }),
      })
    }))
    const settle = () => vi.advanceTimersByTimeAsync(0)
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "1")
    await vi.advanceTimersByTimeAsync(1_000)
    window.dispatchEvent(new Event("pagehide"))
    writeRaw("qui-test-bool", "2")
    expect(puts).toHaveLength(2)

    puts[0].commit()
    puts[0].answer()
    await settle()
    await vi.advanceTimersByTimeAsync(1_000)
    expect(puts).toHaveLength(3)
    puts[2].commit()
    puts[1].commit()
    puts[1].answer()
    await settle()
    puts[2].answer()
    await settle()
    await vi.advanceTimersByTimeAsync(1_000)
    for (let i = 3; i < puts.length; i++) {
      puts[i].commit()
      puts[i].answer()
      await settle()
    }

    expect(server["qui-test-bool"]).toBe("2")
  })

  it.each([
    ["pagehide", () => window.dispatchEvent(new Event("pagehide"))],
    ["a hidden tab", () => {
      vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
      document.dispatchEvent(new Event("visibilitychange"))
    }],
  ])("splits the save at %s into keepalive PUTs under 64 KiB", async (_name, hidePage) => {
    const values = storeLargeLocalSetup()
    const server = fakeServer()
    seedAndMarkReady({})

    hidePage()
    await vi.runAllTimersAsync()

    expect(fetchMock.mock.calls.length).toBeGreaterThan(1)
    for (const [, init] of fetchMock.mock.calls) expect(init.keepalive).toBe(true)
    for (const size of bodySizes()) expect(size).toBeLessThanOrEqual(MAX_BODY_BYTES)
    expect(server).toEqual(values)
  })
})

describe("outbox persistence", () => {
  it("replays a write whose unload flush never reached the server", async () => {
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")

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
  it.each([
    ["normal", "normal", "normal"],
    ["dense", "compact", "dense"],
    ["compact", "compact", "dense"],
    ["ultra-compact", "ultra-compact", "dense"],
  ])("migrates legacy torrent view mode %s", (legacy, mobile, desktop) => {
    const serverSettings = { "qui-torrent-view-mode": legacy }
    applyServerSettings(serverSettings)

    seedAndMarkReady(serverSettings)

    expect(localStorage.getItem("qui-torrent-mobile-view-mode")).toBe(mobile)
    expect(localStorage.getItem("qui-torrent-desktop-view-mode")).toBe(desktop)
  })

  it("does not overwrite layout-specific torrent view modes during migration", () => {
    const serverSettings = {
      "qui-torrent-view-mode": "compact",
      "qui-torrent-mobile-view-mode": "normal",
    }
    applyServerSettings(serverSettings)

    seedAndMarkReady(serverSettings)

    expect(localStorage.getItem("qui-torrent-mobile-view-mode")).toBe("normal")
    expect(localStorage.getItem("qui-torrent-desktop-view-mode")).toBe("dense")
  })

  it("pushes synced keys the server does not know, and only those", async () => {
    localStorage.setItem("qui-speed-units", "bits")
    localStorage.setItem("qui-incognito-mode", "true")
    localStorage.setItem("qui-torrent-mobile-view-mode", "ultra-compact")
    localStorage.setItem("theme-cache", "{}")

    seedAndMarkReady({ "qui-incognito-mode": "true" })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      "qui-speed-units": "bits",
      "qui-torrent-mobile-view-mode": "ultra-compact",
    })
  })
})

describe("display profiles", () => {
  const visibility = { defaultValue: {} as Record<string, boolean>, parse: (raw: string) => JSON.parse(raw) as Record<string, boolean> }
  const filters = { defaultValue: {} as Record<string, string[]>, parse: (raw: string) => JSON.parse(raw) as Record<string, string[]> }
  const sorting = { defaultValue: [] as unknown[], parse: (raw: string) => JSON.parse(raw) as unknown[] }

  it("Default reads the keys of today", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: false }))

    expect(getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-column-visibility:1", visibility)).toEqual({ ratio: false })
  })

  it("a Display setting that Laptop writes does not change Default", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)
    writeClientSetting("qui-column-visibility:1", { ratio: true })
    const laptop = createDisplayProfile("Laptop")

    writeClientSetting("qui-column-visibility:1", { ratio: false })

    expect(getDisplayProfiles().active.id).toBe(laptop)
    expect(localStorage.getItem(`qui-display-profile:${laptop}:qui-column-visibility:1`)).toBe(JSON.stringify({ ratio: false }))
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-column-visibility:1", visibility)).toEqual({ ratio: true })
  })

  it("a Shared setting and a Sort and filters setting are the same on every profile", () => {
    createDisplayProfile("Laptop")
    writeClientSetting("qui-speed-units", "bits", String)
    writeClientSetting("qui-column-sorting:1", [{ id: "ratio", desc: true }])

    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)

    expect(readClientSetting("qui-speed-units", { defaultValue: "", parse: String })).toBe("bits")
    expect(readClientSetting("qui-column-sorting:1", { defaultValue: [], parse: JSON.parse })).toEqual([{ id: "ratio", desc: true }])
  })

  it("create copies the Display settings of every instance of the active profile, and only those", () => {
    localStorage.setItem("qui-column-visibility:1", "{\"ratio\":false}")
    localStorage.setItem("qui-column-sizing:2", "{\"name\":600}")
    localStorage.setItem("qui-torrent-desktop-view-mode", "dense")
    localStorage.setItem("qui-column-sorting:1", "[]")
    localStorage.setItem("qui-speed-units", "bits")
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-filter-sidebar-collapsed", true)

    const phone = createDisplayProfile("Phone")

    const phoneKeys = Object.keys(localStorage).filter((key) => key.startsWith(`qui-display-profile:${phone}:`)).sort()
    expect(phoneKeys).toEqual([
      `qui-display-profile:${phone}:qui-column-sizing:2`,
      `qui-display-profile:${phone}:qui-column-visibility:1`,
      `qui-display-profile:${phone}:qui-filter-sidebar-collapsed`,
      `qui-display-profile:${phone}:qui-torrent-desktop-view-mode`,
    ])
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-column-sizing:2`)).toBe("{\"name\":600}")
    expect(localStorage.getItem("qui-filter-sidebar-collapsed")).toBeNull()
    expect(laptop).not.toBe(phone)
  })

  it("a new profile starts with the bookmark icon, no color, Same columns unset, and Own sort and filters off, and Default has no name of its own", () => {
    const laptop = createDisplayProfile("  Laptop ")

    const { profiles, active } = getDisplayProfiles()
    expect(profiles.map((profile) => profile.name)).toEqual(["", "Laptop"])
    expect(profiles[0]).toMatchObject({ isDefault: true, icon: "app-window", color: null, sameColumns: null, ownSortAndFilters: false })
    expect(active).toEqual({
      id: laptop,
      isDefault: false,
      name: "Laptop",
      icon: "bookmark",
      color: null,
      sameColumns: null,
      ownSortAndFilters: false,
      usesSameColumns: true,
    })
  })

  it("set icon and set color save on the profile, and a new profile does not copy them", () => {
    const laptop = createDisplayProfile("Laptop")
    setDisplayProfileIcon(laptop, "laptop")
    setDisplayProfileColor(laptop, "sky")
    setDisplayProfileColor(DEFAULT_DISPLAY_PROFILE_ID, "rose")

    const phone = createDisplayProfile("Phone", { icon: "smartphone" })

    const byId = new Map(getDisplayProfiles().profiles.map((profile) => [profile.id, profile]))
    expect(byId.get(laptop)).toMatchObject({ name: "Laptop", icon: "laptop", color: "sky" })
    expect(byId.get(DEFAULT_DISPLAY_PROFILE_ID)).toMatchObject({ name: "", icon: "app-window", color: "rose" })
    expect(byId.get(phone)).toMatchObject({ name: "Phone", icon: "smartphone", color: null })

    setDisplayProfileColor(laptop, null)
    expect(getDisplayProfiles().profiles.find((profile) => profile.id === laptop)?.color).toBeNull()
  })

  it("create refuses a name that the name rule rejects", () => {
    createDisplayProfile("Laptop")

    expect(() => createDisplayProfile("LAPTOP")).toThrow()
    expect(getDisplayProfiles().profiles).toHaveLength(2)
  })

  it("pushes the profile values and the entry, but never the pick of this browser", async () => {
    seedAndMarkReady({})
    writeClientSetting("qui-sidebar-icon-only", true)
    const laptop = createDisplayProfile("Laptop")
    await vi.runAllTimersAsync()

    const pushed = Object.assign({}, ...fetchMock.mock.calls.map(([, init]) => JSON.parse(init.body)))
    expect(Object.keys(pushed).sort()).toEqual([
      `qui-display-profile-entry:${laptop}`,
      `qui-display-profile:${laptop}:qui-sidebar-icon-only`,
      "qui-sidebar-icon-only",
    ])

    fetchMock.mockClear()
    _resetClientSettingsForTests()
    seedAndMarkReady(pushed)
    await vi.runAllTimersAsync()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(getDisplayProfiles().active.id).toBe(laptop)
  })

  it("two browsers that create a profile at the same time both keep theirs", async () => {
    const server: Record<string, string> = {}
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      return { ok: true, status: 200 }
    })
    seedAndMarkReady({})
    const laptop = createDisplayProfile("Laptop")
    await vi.runAllTimersAsync()

    const browserA = { ...localStorage }
    localStorage.clear()
    _resetClientSettingsForTests()
    seedAndMarkReady({})
    const phone = createDisplayProfile("Phone")
    await vi.runAllTimersAsync()
    applyServerSettings({ ...server })

    expect(getDisplayProfiles().profiles.map((profile) => profile.id)).toEqual([DEFAULT_DISPLAY_PROFILE_ID, laptop, phone])
    localStorage.clear()
    for (const [key, raw] of Object.entries(browserA)) localStorage.setItem(key, raw)
    _resetClientSettingsForTests()
    applyServerSettings({ ...server })
    expect(getDisplayProfiles().profiles.map((profile) => profile.id)).toEqual([DEFAULT_DISPLAY_PROFILE_ID, laptop, phone])
  })

  it("a second browser on the same profile sees the changes of the first", () => {
    const laptop = createDisplayProfile("Laptop")
    const { result } = renderHook(() => useClientSetting("qui-filter-sidebar-collapsed", boolSetting))

    act(() => {
      applyServerSettings({ [`qui-display-profile:${laptop}:qui-filter-sidebar-collapsed`]: "true" })
    })

    expect(result.current[0]).toBe(true)
  })

  it("a pick re-reads every live setting with one render", () => {
    localStorage.setItem("qui-sidebar-icon-only", "false")
    localStorage.setItem("qui-filter-sidebar-collapsed", "false")
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-icon-only", true)
    writeClientSetting("qui-filter-sidebar-collapsed", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    let renders = 0
    const { result } = renderHook(() => {
      renders++
      return [useClientSetting("qui-sidebar-icon-only", boolSetting)[0], useClientSetting("qui-filter-sidebar-collapsed", boolSetting)[0]]
    })
    expect(result.current).toEqual([false, false])
    renders = 0

    act(() => pickDisplayProfile(laptop))

    expect(result.current).toEqual([true, true])
    expect(renders).toBe(1)
  })

  it("with Own sort and filters on, a Sort and filters setting that Phone writes does not change Default", () => {
    writeClientSetting("qui-filters-global", { status: ["all"] })
    const phone = createDisplayProfile("Phone")

    setDisplayProfileOwnSortAndFilters(phone, true)
    writeClientSetting("qui-filters-global", { status: ["downloading"] })

    expect(getDisplayProfiles().active).toMatchObject({ id: phone, ownSortAndFilters: true })
    expect(readClientSetting("qui-filters-global", filters)).toEqual({ status: ["downloading"] })
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-filters-global", filters)).toEqual({ status: ["all"] })
  })

  it("turning Own sort and filters on copies the shared values, so nothing changes at that moment", () => {
    writeClientSetting("qui-column-sorting:1", [{ id: "ratio", desc: true }])
    writeClientSetting("qui-selected-instance-backups", 2)
    writeClientSetting("qui-speed-units", "bits", String)
    const phone = createDisplayProfile("Phone")
    const { result } = renderHook(() => useClientSetting("qui-column-sorting:1", sorting))

    act(() => setDisplayProfileOwnSortAndFilters(phone, true))

    expect(result.current[0]).toEqual([{ id: "ratio", desc: true }])
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-column-sorting:1`)).toBe("[{\"id\":\"ratio\",\"desc\":true}]")
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-selected-instance-backups`)).toBe("2")
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-speed-units`)).toBeNull()
  })

  it("turning Own sort and filters off blanks the own values, and the profile reads the shared values again", () => {
    writeClientSetting("qui-column-sorting:1", [{ id: "ratio", desc: true }])
    const phone = createDisplayProfile("Phone")
    setDisplayProfileOwnSortAndFilters(phone, true)
    writeClientSetting("qui-column-sorting:1", [{ id: "name", desc: false }])
    writeClientSetting("qui-column-filters-1", [{ id: "state", value: "error" }])
    const { result } = renderHook(() => useClientSetting("qui-column-sorting:1", sorting))

    act(() => setDisplayProfileOwnSortAndFilters(phone, false))

    expect(getDisplayProfiles().active.ownSortAndFilters).toBe(false)
    expect(result.current[0]).toEqual([{ id: "ratio", desc: true }])
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-column-sorting:1`)).toBe("")
    expect(localStorage.getItem(`qui-display-profile:${phone}:qui-column-filters-1`)).toBe("")
  })

  it("Default always shares sort and filters", () => {
    expect(() => setDisplayProfileOwnSortAndFilters(DEFAULT_DISPLAY_PROFILE_ID, true)).toThrow()
    expect(getDisplayProfiles().active.ownSortAndFilters).toBe(false)
  })

  it("create copies Own sort and filters and the own values of the source, but not the icon or the color", () => {
    writeClientSetting("qui-group-copies", false)
    const phone = createDisplayProfile("Phone")
    setDisplayProfileOwnSortAndFilters(phone, true)
    writeClientSetting("qui-group-copies", true)

    createDisplayProfile("Tablet")

    expect(getDisplayProfiles().active).toMatchObject({ name: "Tablet", ownSortAndFilters: true, icon: "bookmark", color: null })
    expect(readClientSetting("qui-group-copies", boolSetting)).toBe(true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-group-copies", boolSetting)).toBe(false)
  })

  it("useDisplayProfiles follows a pick made in another tab", () => {
    const laptop = createDisplayProfile("Laptop")
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    const { result } = renderHook(() => useDisplayProfiles())
    expect(result.current.active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)

    localStorage.setItem("qui-display-profile-active", laptop)
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: "qui-display-profile-active" }))
    })

    expect(result.current.active.id).toBe(laptop)
  })

  it("delete writes a tombstone, blanks the entry and every value of the profile, and pushes them", async () => {
    seedAndMarkReady({})
    localStorage.setItem("qui-column-visibility:1", "{\"ratio\":false}")
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-icon-only", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    await vi.runAllTimersAsync()
    fetchMock.mockClear()

    deleteDisplayProfile(laptop)
    await vi.runAllTimersAsync()

    const blanked = {
      [`qui-display-profile-deleted:${laptop}`]: "1",
      [`qui-display-profile-entry:${laptop}`]: "",
      [`qui-display-profile:${laptop}:qui-column-visibility:1`]: "",
      [`qui-display-profile:${laptop}:qui-sidebar-icon-only`]: "",
    }
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual(blanked)
    for (const [key, raw] of Object.entries(blanked)) expect(localStorage.getItem(key)).toBe(raw)
    expect(localStorage.getItem("qui-column-visibility:1")).toBe("{\"ratio\":false}")
    expect(getDisplayProfiles().profiles.map((profile) => profile.id)).toEqual([DEFAULT_DISPLAY_PROFILE_ID])
  })

  it("delete refuses Default", () => {
    writeClientSetting("qui-sidebar-icon-only", true)

    expect(() => deleteDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)).toThrow()
    expect(localStorage.getItem("qui-sidebar-icon-only")).toBe("true")
    expect(localStorage.getItem("qui-display-profile-entry:default")).toBeNull()
  })

  it("a browser that deletes the profile that it uses moves to Default", () => {
    writeClientSetting("qui-sidebar-icon-only", false)
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-icon-only", true)
    const { result } = renderHook(() => useClientSetting("qui-sidebar-icon-only", boolSetting))
    expect(result.current[0]).toBe(true)

    act(() => deleteDisplayProfile(laptop))

    expect(localStorage.getItem("qui-display-profile-active")).toBe(DEFAULT_DISPLAY_PROFILE_ID)
    expect(result.current[0]).toBe(false)
  })

  it("a browser on a profile that another browser deleted moves to Default, drops its unsaved writes, and keeps the outbox right", async () => {
    writeClientSetting("qui-sidebar-icon-only", false)
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-icon-only", true)
    writeClientSetting("qui-speed-units", "bits", String)
    const deleted = vi.fn()
    const unsubscribe = onActiveDisplayProfileDeleted(deleted)
    const { result } = renderHook(() => useClientSetting("qui-sidebar-icon-only", boolSetting))

    const snapshot = { [`qui-display-profile-entry:${laptop}`]: "", [`qui-display-profile:${laptop}:qui-sidebar-icon-only`]: "" }
    act(() => {
      applyServerSettings(snapshot)
    })
    unsubscribe()

    expect(localStorage.getItem("qui-display-profile-active")).toBe(DEFAULT_DISPLAY_PROFILE_ID)
    expect(result.current[0]).toBe(false)
    expect(deleted).toHaveBeenCalledTimes(1)
    expect(deleted.mock.calls[0][0]).toMatchObject({ id: laptop, name: "Laptop" })
    const outbox = JSON.parse(localStorage.getItem("qui-client-settings-outbox") ?? "[]") as string[]
    expect(outbox.sort()).toEqual(["qui-sidebar-icon-only", "qui-speed-units"])

    seedAndMarkReady(snapshot)
    await vi.runAllTimersAsync()
    expect(Object.keys(JSON.parse(fetchMock.mock.calls[0][1].body)).sort()).toEqual(["qui-sidebar-icon-only", "qui-speed-units"])
  })

  describe("a second tab of the browser", () => {
    async function openSecondTab() {
      vi.resetModules()
      return await import("@/lib/client-settings")
    }

    function sendStorageEvents(keys: string[]) {
      for (const key of keys) window.dispatchEvent(new StorageEvent("storage", { key }))
    }

    it("tells the user when the first tab applies a server delete of its profile", async () => {
      const laptop = createDisplayProfile("Laptop")
      const tabB = await openSecondTab()
      expect(tabB.getDisplayProfiles().active.id).toBe(laptop)
      const deletedInB = vi.fn()
      tabB.onActiveDisplayProfileDeleted(deletedInB)

      const snapshot = { [`qui-display-profile-deleted:${laptop}`]: "1", [`qui-display-profile-entry:${laptop}`]: "" }
      applyServerSettings(snapshot)
      sendStorageEvents([...Object.keys(snapshot), "qui-display-profile-active"])

      expect(tabB.getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)
      expect(deletedInB).toHaveBeenCalledTimes(1)
      expect(deletedInB.mock.calls[0][0]).toMatchObject({ id: laptop, name: "Laptop" })
    })

    it("does not tell the user when the first tab deletes the profile itself", async () => {
      const laptop = createDisplayProfile("Laptop")
      const tabB = await openSecondTab()
      expect(tabB.getDisplayProfiles().active.id).toBe(laptop)
      const deletedInB = vi.fn()
      tabB.onActiveDisplayProfileDeleted(deletedInB)

      deleteDisplayProfile(laptop)
      sendStorageEvents([`qui-display-profile-deleted:${laptop}`, `qui-display-profile-entry:${laptop}`, "qui-display-profile-active"])

      expect(tabB.getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)
      expect(deletedInB).not.toHaveBeenCalled()
    })
  })

  it("a browser that still has a local copy of a deleted profile does not bring it back on a page load", async () => {
    const server: Record<string, string> = {}
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      return { ok: true, status: 200 }
    })
    seedAndMarkReady({})
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-icon-only", true)
    await vi.runAllTimersAsync()
    const browserA = { ...localStorage }
    deleteDisplayProfile(laptop)
    await vi.runAllTimersAsync()

    localStorage.clear()
    for (const [key, raw] of Object.entries(browserA)) localStorage.setItem(key, raw)
    renameDisplayProfile(laptop, "Old laptop")
    writeClientSetting("qui-column-order:2", ["name"])
    localStorage.setItem(`qui-display-profile:${laptop}:qui-column-sizing:2`, "{\"name\":600}")
    _resetClientSettingsForTests()
    fetchMock.mockClear()

    applyServerSettings({ ...server })
    seedAndMarkReady({ ...server })
    await vi.runAllTimersAsync()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(server[`qui-display-profile-entry:${laptop}`]).toBe("")
    expect(getDisplayProfiles().profiles.map((profile) => profile.id)).toEqual([DEFAULT_DISPLAY_PROFILE_ID])
    expect(getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)
  })

  it("an edit that reaches the server after a delete does not bring the profile back", async () => {
    seedAndMarkReady({})
    const laptop = createDisplayProfile("Laptop")
    const laptopEntry = localStorage.getItem(`qui-display-profile-entry:${laptop}`)!
    await vi.runAllTimersAsync()
    fetchMock.mockClear()
    deleteDisplayProfile(laptop)
    await vi.runAllTimersAsync()
    const deletion = JSON.parse(fetchMock.mock.calls[0][1].body) as Record<string, string>

    localStorage.clear()
    _resetClientSettingsForTests()
    applyServerSettings({ [`qui-display-profile-entry:${laptop}`]: laptopEntry })
    applyServerSettings(deletion)
    applyServerSettings({ ...deletion, [`qui-display-profile-entry:${laptop}`]: JSON.stringify({ ...JSON.parse(laptopEntry), name: "Old laptop" }) })

    expect(getDisplayProfiles().profiles.map((profile) => profile.id)).toEqual([DEFAULT_DISPLAY_PROFILE_ID])
  })

  it("rename keeps the rest of the entry and refuses a name that the name rule rejects", () => {
    const laptop = createDisplayProfile("Laptop")
    createDisplayProfile("Phone")

    renameDisplayProfile(laptop, "LAPTOP")
    renameDisplayProfile(laptop, " laptop 13 ")
    renameDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID, "Workstation")

    expect(() => renameDisplayProfile(laptop, "PHONE")).toThrow()
    expect(() => renameDisplayProfile(laptop, " ")).toThrow()
    expect(getDisplayProfiles().profiles.map(({ id, name, icon }) => ({ id, name, icon }))).toEqual([
      { id: DEFAULT_DISPLAY_PROFILE_ID, name: "Workstation", icon: "app-window" },
      { id: laptop, name: "laptop 13", icon: "bookmark" },
      { id: expect.any(String), name: "Phone", icon: "bookmark" },
    ])
  })

  it("an edit keeps the fields of the entry that this version does not know", () => {
    const laptop = createDisplayProfile("Laptop")
    const entryKey = `qui-display-profile-entry:${laptop}`
    localStorage.setItem(entryKey, JSON.stringify({ ...JSON.parse(localStorage.getItem(entryKey)!), pinned: true }))
    _resetClientSettingsForTests()

    renameDisplayProfile(laptop, "Old laptop")
    setDisplayProfileIcon(laptop, "laptop")
    setDisplayProfileOwnSortAndFilters(laptop, true)
    setDisplayProfileSameColumns(laptop, true, 1)

    expect(JSON.parse(localStorage.getItem(entryKey)!)).toEqual({
      name: "Old laptop",
      icon: "laptop",
      color: null,
      sameColumns: true,
      ownSortAndFilters: true,
      pinned: true,
    })
  })

  it("a rename shows in another browser", async () => {
    const server: Record<string, string> = {}
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      return { ok: true, status: 200 }
    })
    seedAndMarkReady({})
    const laptop = createDisplayProfile("Laptop")
    await vi.runAllTimersAsync()
    const browserB = { ...localStorage }
    renameDisplayProfile(laptop, "Workstation")
    renameDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID, "Home")
    await vi.runAllTimersAsync()

    localStorage.clear()
    for (const [key, raw] of Object.entries(browserB)) localStorage.setItem(key, raw)
    _resetClientSettingsForTests()
    const { result } = renderHook(() => useDisplayProfiles())
    expect(result.current.active.name).toBe("Laptop")

    act(() => {
      applyServerSettings({ ...server })
    })

    expect(result.current.profiles.map((profile) => profile.name)).toEqual(["Home", "Workstation"])
    expect(result.current.active.name).toBe("Workstation")
  })

  it("useDisplayProfileSetting reads a Display setting of a profile that this browser does not use, and follows a change", () => {
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-filter-sidebar-collapsed", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    const { result } = renderHook(() => ({
      laptop: useDisplayProfileSetting(laptop, "qui-filter-sidebar-collapsed", boolSetting),
      default: useDisplayProfileSetting(DEFAULT_DISPLAY_PROFILE_ID, "qui-filter-sidebar-collapsed", boolSetting),
    }))
    expect(result.current).toEqual({ laptop: true, default: false })

    act(() => pickDisplayProfile(laptop))
    act(() => writeClientSetting("qui-filter-sidebar-collapsed", false))

    expect(result.current).toEqual({ laptop: false, default: false })
  })
})

describe("same columns on every instance", () => {
  const visibility = { defaultValue: {} as Record<string, boolean>, parse: (raw: string) => JSON.parse(raw) as Record<string, boolean> }
  const sorting = { defaultValue: [] as unknown[], parse: (raw: string) => JSON.parse(raw) as unknown[] }
  const sameColumnsOf = (id: string) => getDisplayProfiles().profiles.find((profile) => profile.id === id)?.usesSameColumns

  it("is on when the profile has no stored Column setup, and off when it has one, until the user sets it", () => {
    expect(getDisplayProfiles().active.usesSameColumns).toBe(true)

    writeRaw("qui-column-sizing:2", "{\"name\":600}")
    expect(getDisplayProfiles().active.usesSameColumns).toBe(false)

    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true)
    expect(getDisplayProfiles().active.usesSameColumns).toBe(true)
  })

  it("with the switch on, every instance and the Unified view read one Column setup, and sort stays per instance", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
    const laptop = createDisplayProfile("Laptop")
    setDisplayProfileSameColumns(laptop, true, 1)

    writeClientSetting("qui-column-visibility:1", { ratio: false })
    writeClientSetting("qui-column-sorting:1", [{ id: "ratio", desc: true }])

    expect(readClientSetting("qui-column-visibility:2", visibility)).toEqual({ ratio: false })
    expect(readClientSetting("qui-column-visibility:0", visibility)).toEqual({ ratio: false })
    expect(readClientSetting("qui-column-sorting:2", sorting)).toEqual([])
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-column-visibility:2", visibility)).toEqual({})
    expect(sameColumnsOf(DEFAULT_DISPLAY_PROFILE_ID)).toBe(false)
  })

  it("a live hook of another instance follows a change while the switch is on", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true)
    const { result } = renderHook(() => useClientSetting("qui-column-visibility:2", visibility))

    act(() => writeClientSetting("qui-column-visibility:1", { ratio: false }))

    expect(result.current[0]).toEqual({ ratio: false })
  })

  it("with the switch off, a live hook of an instance without its own setup follows the shared setup", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)
    const { result } = renderHook(() => useClientSetting("qui-column-visibility:2", visibility))

    act(() => {
      applyServerSettings({ "qui-column-visibility:every-instance": JSON.stringify({ ratio: false }) })
    })

    expect(result.current[0]).toEqual({ ratio: false })
  })

  it("turning it on copies the Column setup of the instance on screen to every instance", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: false }))
    localStorage.setItem("qui-column-visibility:2", JSON.stringify({ size: false }))
    localStorage.setItem("qui-stretch-name-column:2", "false")

    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true, 2)

    expect(readClientSetting("qui-column-visibility:1", visibility)).toEqual({ size: false })
    expect(readClientSetting("qui-column-visibility:0", visibility)).toEqual({ size: false })
    expect(readClientSetting("qui-stretch-name-column:1", boolSetting)).toBe(false)
  })

  it("turning it on from a place with no instance copies the last instance that this browser showed", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: false }))
    localStorage.setItem("qui-column-visibility:2", JSON.stringify({ size: false }))
    recordColumnSetupInstance(2)
    recordColumnSetupInstance(1)
    const { result } = renderHook(() => useLastColumnSetupInstance())
    expect(result.current).toBe(1)

    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true)

    expect(readClientSetting("qui-column-visibility:2", visibility)).toEqual({ ratio: false })
  })

  it("turning it off copies the shared setup to each instance, and each instance then changes on its own", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
    localStorage.setItem("qui-column-visibility:2", JSON.stringify({ size: false }))
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true, 1)
    writeClientSetting("qui-column-visibility:1", { ratio: false })
    writeClientSetting("qui-column-sizing:1", { name: 600 })

    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)

    for (const instance of [1, 2, 3, 0]) {
      expect(readClientSetting(`qui-column-visibility:${instance}`, visibility)).toEqual({ ratio: false })
      expect(readClientSetting(`qui-column-sizing:${instance}`, visibility)).toEqual({ name: 600 })
    }
    writeClientSetting("qui-column-visibility:1", { ratio: true })
    writeClientSetting("qui-column-visibility:3", { tags: false })
    expect(readClientSetting("qui-column-visibility:2", visibility)).toEqual({ ratio: false })
    expect(readClientSetting("qui-column-visibility:3", visibility)).toEqual({ tags: false })
    expect(getDisplayProfiles().active.usesSameColumns).toBe(false)
  })

  it("an instance that had no Column setup before the switch went on shows the defaults after it goes off", () => {
    localStorage.setItem("qui-column-visibility:2", JSON.stringify({ size: false }))
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true, 1)

    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)

    expect(readClientSetting("qui-column-visibility:2", visibility)).toEqual({})
    expect(readClientSetting("qui-column-visibility:1", visibility)).toEqual({})
  })

  it("create copies the switch and the shared setup", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true, 1)
    writeClientSetting("qui-column-visibility:1", { ratio: false })

    const laptop = createDisplayProfile("Laptop")

    expect(sameColumnsOf(laptop)).toBe(true)
    expect(readClientSetting("qui-column-visibility:4", visibility)).toEqual({ ratio: false })
  })

  it("useDisplayProfiles follows the computed switch when the first Column setup of an instance arrives", () => {
    const { result } = renderHook(() => useDisplayProfiles())
    expect(result.current.active.usesSameColumns).toBe(true)

    act(() => {
      applyServerSettings({ "qui-column-sizing:2": "{\"name\":600}" })
    })

    expect(result.current.active.usesSameColumns).toBe(false)
  })

  it("a server snapshot reads localStorage a number of times that grows in step with its size", () => {
    renderHook(() => [
      useDisplayProfiles(),
      useClientSetting("qui-column-visibility:1", visibility),
      useClientSetting("qui-sidebar-icon-only", boolSetting),
    ])
    const snapshot: Record<string, string> = { "qui-display-profile-entry:laptop": JSON.stringify({ name: "Laptop" }) }
    for (let instance = 0; instance < 50; instance++) {
      for (const prefix of ["qui-column-visibility:", "qui-column-order:", "qui-column-sizing:", "qui-column-sorting:"]) {
        snapshot[`${prefix}${instance}`] = "{}"
        snapshot[`qui-display-profile:laptop:${prefix}${instance}`] = "{}"
      }
    }
    const keyCount = Object.keys(snapshot).length
    const reads = vi.spyOn(Storage.prototype, "key")

    act(() => {
      applyServerSettings(snapshot)
    })

    expect(reads.mock.calls.length).toBeLessThan(10 * keyCount)
  })

  it("useDisplayProfiles sets the switch", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
    const { result } = renderHook(() => useDisplayProfiles())
    expect(result.current.active.usesSameColumns).toBe(false)

    act(() => result.current.setSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true, 1))

    expect(result.current.active.usesSameColumns).toBe(true)
  })
})

describe("migrateLegacyLocalValue", () => {
  it("moves a valid legacy value into the active profile once, then drops the legacy key", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)
    const laptop = createDisplayProfile("Laptop")
    localStorage.setItem("qui-column-sizing", "{\"name\":600}")

    migrateLegacyLocalValue("qui-column-sizing", "qui-column-sizing:1", () => true)

    expect(localStorage.getItem(`qui-display-profile:${laptop}:qui-column-sizing:1`)).toBe("{\"name\":600}")
    expect(localStorage.getItem("qui-column-sizing:1")).toBeNull()
    expect(localStorage.getItem("qui-column-sizing")).toBeNull()
  })

  it("keeps the legacy key when the full storage refuses the new value", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)
    localStorage.setItem("qui-column-sizing", "{\"name\":600}")
    const setItem = Storage.prototype.setItem
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (this: Storage, key: string, value: string) {
      if (key === "qui-column-sizing:1") throw new DOMException("quota", "QuotaExceededError")
      setItem.call(this, key, value)
    })

    migrateLegacyLocalValue("qui-column-sizing", "qui-column-sizing:1", () => true)

    expect(localStorage.getItem("qui-column-sizing:1")).toBeNull()
    expect(localStorage.getItem("qui-column-sizing")).toBe("{\"name\":600}")
  })

  it("keeps a stored value and skips an invalid legacy value", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, false)
    writeClientSetting("qui-column-sizing:1", { name: 300 })
    localStorage.setItem("qui-column-sizing", "{\"name\":600}")
    migrateLegacyLocalValue("qui-column-sizing", "qui-column-sizing:1", () => true)
    localStorage.setItem("qui-column-sizing", "nope")
    migrateLegacyLocalValue("qui-column-sizing", "qui-column-sizing:2", () => false)

    expect(localStorage.getItem("qui-column-sizing:1")).toBe("{\"name\":300}")
    expect(localStorage.getItem("qui-column-sizing:2")).toBeNull()
    expect(localStorage.getItem("qui-column-sizing")).toBeNull()
  })
})
