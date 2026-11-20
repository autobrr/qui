/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { _resetClientSettingsForTests, setDisplayProfileSameColumns } from "@/lib/client-settings"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"
import { usePersistedColumnOrder } from "./usePersistedColumnOrder"

const KEY = "qui-column-order:1"

// Shaped like the real default: select first, tracker_icon after priority, state before dlspeed.
const DEFAULT = [
  "select",
  "priority",
  "tracker_icon",
  "name",
  "size",
  "progress",
  "status_icon",
  "state",
  "dlspeed",
  "upspeed",
  "ratio",
]

function without(order: string[], ...ids: string[]): string[] {
  return order.filter(id => !ids.includes(id))
}

function readOrder(saved: unknown, defaultOrder: string[] = DEFAULT): string[] {
  if (saved !== undefined) {
    window.localStorage.setItem(KEY, typeof saved === "string" ? saved : JSON.stringify(saved))
  }
  const { result } = renderHook(() => usePersistedColumnOrder(defaultOrder, 1))
  return result.current[0]
}

afterEach(() => {
  cleanup()
  window.localStorage.clear()
  _resetClientSettingsForTests()
})

describe("usePersistedColumnOrder", () => {
  it("uses the default order when nothing is saved", () => {
    expect(readOrder(undefined)).toEqual(DEFAULT)
  })

  it.each([
    ["invalid JSON", "not json"],
    ["an object", { name: 0 }],
    ["an array with a non-string", ["name", 3]],
  ])("falls back to the default order on %s", (_label, saved) => {
    expect(readOrder(saved)).toEqual(DEFAULT)
  })

  it("rebuilds the default order from an empty saved order", () => {
    expect(readOrder([])).toEqual(DEFAULT)
  })

  it("returns a complete saved order as saved, stale ids included", () => {
    const saved = ["gone", ...[...DEFAULT].reverse()]
    expect(readOrder(saved)).toEqual(saved)
  })

  it("puts a missing column right after its default predecessor, wherever the user moved it", () => {
    const saved = ["select", "priority", "tracker_icon", "progress", "status_icon", "state", "dlspeed", "upspeed", "ratio", "name"]
    expect(readOrder(saved)).toEqual([...saved, "size"])
  })

  it("keeps several missing columns in default order", () => {
    const saved = ["ratio", "upspeed", "dlspeed", "state", "select", "priority", "tracker_icon", "progress"]
    expect(readOrder(saved)).toEqual([
      "ratio", "upspeed", "dlspeed", "state", "select", "priority", "tracker_icon", "name", "size", "progress", "status_icon",
    ])
  })

  it("re-adds a missing select column first", () => {
    expect(readOrder(without(DEFAULT, "select"))).toEqual(DEFAULT)
  })

  it("does not write the merged order back to storage", () => {
    const saved = JSON.stringify(without(DEFAULT, "size"))
    readOrder(saved)
    expect(window.localStorage.getItem(KEY)).toBe(saved)
  })

  // TorrentTableRow's memo compares columnOrder with Object.is, and the table passes a new default array each render.
  it("keeps the same array across renders", () => {
    window.localStorage.setItem(KEY, JSON.stringify(without(DEFAULT, "size")))
    const { result, rerender } = renderHook(() => usePersistedColumnOrder([...DEFAULT], 1))
    const first = result.current[0]
    rerender()
    expect(result.current[0]).toBe(first)
  })

  it("with Same columns on every instance, the Unified view adds its Instance column to the order of an instance", () => {
    setDisplayProfileSameColumns(DEFAULT_DISPLAY_PROFILE_ID, true)
    const instance = renderHook(() => usePersistedColumnOrder(DEFAULT, 1))
    act(() => instance.result.current[1](["name", ...without(DEFAULT, "name")]))

    const unified = renderHook(() => usePersistedColumnOrder(["select", "name", "instance", ...without(DEFAULT, "select", "name")], 0))

    expect(unified.result.current[0]).toEqual(["name", "instance", ...without(DEFAULT, "name")])
  })
})
