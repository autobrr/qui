/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { makeFilters } from "@/test/mockFilters"
import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const GLOBAL_KEY = "qui-filters-global"
const instanceKey = (id: number) => `qui-filters-${id}`

function readJSON(key: string): unknown {
  const raw = localStorage.getItem(key)
  return raw === null ? null : JSON.parse(raw)
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe("usePersistedFilters", () => {
  it("lazy-inits to all-empty defaults when localStorage is empty", () => {
    const { result } = renderHook(() => usePersistedFilters(1))
    const [filters] = result.current

    // expr defaults to "" which makeFilters does not set, so override it.
    expect(filters).toEqual(makeFilters({ expr: "" }))
    // Pin the expr-default contract directly so a regression that stops
    // defaulting expr is caught independently of the structural compare.
    expect(filters.expr).toBe("")
  })

  it("hydrates by merging global and per-instance stored filters", () => {
    localStorage.setItem(GLOBAL_KEY, JSON.stringify({
      status: ["downloading"],
      excludeStatus: ["paused"],
    }))
    localStorage.setItem(instanceKey(7), JSON.stringify({
      categories: ["movies"],
      excludeCategories: ["tv"],
      tags: ["hd"],
      excludeTags: ["sd"],
      trackers: ["tracker-a"],
      excludeTrackers: ["tracker-b"],
      expr: "size > 1GB",
    }))

    const { result } = renderHook(() => usePersistedFilters(7))
    const [filters] = result.current

    expect(filters).toEqual(makeFilters({
      status: ["downloading"],
      excludeStatus: ["paused"],
      categories: ["movies"],
      excludeCategories: ["tv"],
      tags: ["hd"],
      excludeTags: ["sd"],
      trackers: ["tracker-a"],
      excludeTrackers: ["tracker-b"],
      expr: "size > 1GB",
    }))
  })

  it("persists only status/excludeStatus to the global key", () => {
    const { result } = renderHook(() => usePersistedFilters(3))

    act(() => {
      const [, setFilters] = result.current
      setFilters(prev => ({
        ...prev,
        status: ["seeding"],
        excludeStatus: ["errored"],
      }))
    })

    // Global key carries ONLY the cross-instance status fields.
    expect(readJSON(GLOBAL_KEY)).toEqual({
      status: ["seeding"],
      excludeStatus: ["errored"],
    })

    // Returned state reflects the update.
    expect(result.current[0].status).toEqual(["seeding"])
    expect(result.current[0].excludeStatus).toEqual(["errored"])
  })

  it("persists only instance-scoped fields to the per-instance key", () => {
    const { result } = renderHook(() => usePersistedFilters(9))

    act(() => {
      const [, setFilters] = result.current
      setFilters(prev => ({
        ...prev,
        categories: ["iso"],
        excludeCategories: ["junk"],
        tags: ["seeded"],
        excludeTags: ["stalled"],
        trackers: ["t1"],
        excludeTrackers: ["t2"],
        expr: "ratio < 1",
      }))
    })

    // Instance key carries only the instance-scoped fields (no status fields).
    expect(readJSON(instanceKey(9))).toEqual({
      categories: ["iso"],
      excludeCategories: ["junk"],
      tags: ["seeded"],
      excludeTags: ["stalled"],
      trackers: ["t1"],
      excludeTrackers: ["t2"],
      expr: "ratio < 1",
    })

    // Global key holds empty status arrays, untouched by instance-scoped changes.
    expect(readJSON(GLOBAL_KEY)).toEqual({
      status: [],
      excludeStatus: [],
    })
  })

  it("isolates instance fields across instances but shares the global status", () => {
    const hook1 = renderHook(() => usePersistedFilters(1))
    act(() => {
      const [, setFilters] = hook1.result.current
      setFilters(prev => ({
        ...prev,
        status: ["downloading"],
        categories: ["instance-1-cat"],
      }))
    })

    // A second instance mounts and reads from storage. It inherits the shared
    // global status but must NOT see instance 1's categories.
    const hook2 = renderHook(() => usePersistedFilters(2))
    const [filters2] = hook2.result.current

    expect(filters2.status).toEqual(["downloading"])
    expect(filters2.categories).toEqual([])
  })

  it("reloads instance fields when instanceId changes via rerender", () => {
    localStorage.setItem(instanceKey(1), JSON.stringify({ categories: ["one"] }))
    localStorage.setItem(instanceKey(2), JSON.stringify({ categories: ["two"] }))
    localStorage.setItem(GLOBAL_KEY, JSON.stringify({ status: ["downloading"] }))

    const { result, rerender } = renderHook(
      ({ id }: { id: number }) => usePersistedFilters(id),
      { initialProps: { id: 1 } }
    )

    expect(result.current[0].categories).toEqual(["one"])
    expect(result.current[0].status).toEqual(["downloading"])

    act(() => {
      rerender({ id: 2 })
    })

    // The instanceId-keyed effect reloads from qui-filters-2 while global persists.
    expect(result.current[0].categories).toEqual(["two"])
    expect(result.current[0].status).toEqual(["downloading"])
  })

  // ERROR PATH: corrupt per-instance JSON. useClientSetting catches the parse
  // throw and falls back to the empty defaults instead of crashing the tree.
  it("falls back to defaults on corrupt per-instance JSON", () => {
    localStorage.setItem(instanceKey(5), "not-json")

    const { result } = renderHook(() => usePersistedFilters(5))

    expect(result.current[0]).toEqual(makeFilters({ expr: "" }))
  })

  // ERROR PATH: corrupt per-instance JSON reached via an instance switch. The
  // clean instance's values load, the corrupt one degrades to defaults.
  it("falls back to defaults on corrupt per-instance JSON after an instance switch", () => {
    localStorage.setItem(instanceKey(1), JSON.stringify({ categories: ["clean"] }))
    localStorage.setItem(instanceKey(2), "not-json")

    const { result, rerender } = renderHook(
      ({ id }: { id: number }) => usePersistedFilters(id),
      { initialProps: { id: 1 } }
    )

    expect(result.current[0].categories).toEqual(["clean"])

    act(() => {
      rerender({ id: 2 })
    })

    expect(result.current[0].categories).toEqual([])
  })

  // ERROR PATH: corrupt GLOBAL JSON degrades to defaults the same way.
  it("falls back to defaults on corrupt global JSON", () => {
    localStorage.setItem(GLOBAL_KEY, "not-json")

    const { result } = renderHook(() => usePersistedFilters(8))

    expect(result.current[0]).toEqual(makeFilters({ expr: "" }))
  })

  // ERROR PATH: write failure. writeRaw logs and drops the write; the hook
  // must not throw. State follows storage, so the value stays unchanged.
  it("swallows localStorage write failures without throwing", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {})

    const { result } = renderHook(() => usePersistedFilters(4))

    const setItemSpy = vi
      .spyOn(Storage.prototype, "setItem")
      .mockImplementation(() => {
        throw new Error("quota exceeded")
      })

    expect(() => {
      act(() => {
        const [, setFilters] = result.current
        setFilters(prev => ({ ...prev, status: ["seeding"] }))
      })
    }).not.toThrow()

    expect(setItemSpy).toHaveBeenCalled()
    expect(consoleError).toHaveBeenCalled()
    // Storage-backed state keeps the previous value when the write fails.
    expect(result.current[0].status).toEqual([])
  })

  // ERROR PATH: read failure. readRaw returns null on getItem throw, so the
  // hook falls back to empty defaults without throwing.
  it("falls back to empty defaults when localStorage read throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("storage disabled")
    })

    const { result } = renderHook(() => usePersistedFilters(6))

    expect(result.current[0]).toEqual(makeFilters({ expr: "" }))
  })
})
