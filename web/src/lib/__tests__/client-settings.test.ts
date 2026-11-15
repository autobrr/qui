/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  createDisplayProfile,
  getDisplayProfiles,
  migrateLegacyLocalValue,
  pickDisplayProfile,
  applyServerSettings,
  parseJsonBoolean,
  readClientSetting,
  seedAndMarkReady,
  useClientSetting,
  useDisplayProfiles,
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

  it("replays a newer cross-tab value after an older PUT finishes", async () => {
    let resolvePut: (value: unknown) => void = () => {}
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolvePut = resolve }))
    seedAndMarkReady({})
    writeRaw("qui-test-bool", "true")
    await vi.advanceTimersByTimeAsync(1_000)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ "qui-test-bool": "true" })

    // Another tab writes the shared cache while this tab's older PUT is slow.
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

    // Tab B edits mid-flight; its edit clears the mark of the server copy.
    tabB.writeRaw("qui-test-bool", "false")
    resolvePut({ ok: true, status: 200 })
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ "qui-test-bool": "false" })
  })

  // REGRESSION (#3424): another tab wrote a one-write-old server snapshot to
  // the shared localStorage, and this tab's next flush pushed it back as an
  // edit. Each PUT then caused the next, and the value flipped forever.
  it.each([1, 3])("does not push a server copy back after two quick writes, with %i other tabs", async (otherTabCount) => {
    const otherTabs: (typeof import("@/lib/client-settings"))[] = []
    for (let i = 0; i < otherTabCount; i++) {
      vi.resetModules()
      otherTabs.push(await import("@/lib/client-settings"))
    }
    const key = "qui-sidebar-collapsed"
    const server: Record<string, string> = {}
    fetchMock.mockImplementation((_url: string, init: RequestInit) => {
      Object.assign(server, JSON.parse(String(init.body)))
      const snapshot = { ...server }
      // The activity event and the GET reach each tab after the PUT returns,
      // a little later in each other tab.
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

    // A fresh boot of another tab seeds nothing new: the mark is never synced.
    otherTabs[0]._resetClientSettingsForTests()
    otherTabs[0].seedAndMarkReady(server)
    await vi.runAllTimersAsync()
    for (const [, init] of fetchMock.mock.calls) {
      expect(Object.keys(JSON.parse(init.body))).toEqual([key])
    }
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
    localStorage.setItem("theme-cache", "{}") // not a synced key

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

  it("Default reads the keys of today", () => {
    localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: false }))

    expect(getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-column-visibility:1", visibility)).toEqual({ ratio: false })
  })

  it("a Display setting that Laptop writes does not change Default", () => {
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

  it("a new profile starts with the bookmark icon, no color, Same columns unset, and Own sort and filters off", () => {
    const laptop = createDisplayProfile("  Laptop ")

    const { profiles, active } = getDisplayProfiles()
    expect(profiles.map((profile) => profile.name)).toEqual(["Default", "Laptop"])
    expect(profiles[0]).toMatchObject({ isDefault: true, icon: "app-window", color: null, sameColumns: null, ownSortAndFilters: false })
    expect(active).toEqual({ id: laptop, isDefault: false, name: "Laptop", icon: "bookmark", color: null, sameColumns: null, ownSortAndFilters: false })
  })

  it("create refuses a name that the name rule rejects", () => {
    createDisplayProfile("Laptop")

    expect(() => createDisplayProfile("LAPTOP")).toThrow()
    expect(getDisplayProfiles().profiles).toHaveLength(2)
  })

  it("pushes the profile values and the entry, but never the pick of this browser", async () => {
    seedAndMarkReady({})
    writeClientSetting("qui-sidebar-collapsed", true)
    const laptop = createDisplayProfile("Laptop")
    await vi.runAllTimersAsync()

    const pushed = Object.assign({}, ...fetchMock.mock.calls.map(([, init]) => JSON.parse(init.body)))
    expect(Object.keys(pushed).sort()).toEqual([
      `qui-display-profile-entry:${laptop}`,
      `qui-display-profile:${laptop}:qui-sidebar-collapsed`,
      "qui-sidebar-collapsed",
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

    // Browser B has its own localStorage and has not seen Laptop yet.
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
    localStorage.setItem("qui-sidebar-collapsed", "false")
    localStorage.setItem("qui-filter-sidebar-collapsed", "false")
    const laptop = createDisplayProfile("Laptop")
    writeClientSetting("qui-sidebar-collapsed", true)
    writeClientSetting("qui-filter-sidebar-collapsed", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    let renders = 0
    const { result } = renderHook(() => {
      renders++
      return [useClientSetting("qui-sidebar-collapsed", boolSetting)[0], useClientSetting("qui-filter-sidebar-collapsed", boolSetting)[0]]
    })
    expect(result.current).toEqual([false, false])
    renders = 0

    act(() => pickDisplayProfile(laptop))

    expect(result.current).toEqual([true, true])
    expect(renders).toBe(1)
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
})

describe("migrateLegacyLocalValue", () => {
  it("moves a valid legacy value into the active profile once, then drops the legacy key", () => {
    const laptop = createDisplayProfile("Laptop")
    localStorage.setItem("qui-column-sizing", "{\"name\":600}")

    migrateLegacyLocalValue("qui-column-sizing", "qui-column-sizing:1", () => true)

    expect(localStorage.getItem(`qui-display-profile:${laptop}:qui-column-sizing:1`)).toBe("{\"name\":600}")
    expect(localStorage.getItem("qui-column-sizing:1")).toBeNull()
    expect(localStorage.getItem("qui-column-sizing")).toBeNull()
  })

  it("keeps a stored value and skips an invalid legacy value", () => {
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
