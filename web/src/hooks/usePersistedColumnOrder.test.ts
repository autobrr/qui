/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { usePersistedColumnOrder } from "./usePersistedColumnOrder"

// These tests pin today's merge behaviour, quirks included, so a later change shows up as a diff.

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

  // Each re-added anchor (priority, state, dlspeed) steers the columns re-added after it.
  it("does not rebuild the default order from an empty saved order", () => {
    expect(readOrder([])).toEqual([
      "select", "priority", "status_icon", "tracker_icon", "name", "size", "progress", "state", "ratio", "upspeed", "dlspeed",
    ])
  })

  it("returns a complete saved order as saved, stale ids included", () => {
    const saved = ["gone", ...[...DEFAULT].reverse()]
    expect(readOrder(saved)).toEqual(saved)
  })

  it("puts a missing column right after state", () => {
    expect(readOrder(without(DEFAULT, "size"))).toEqual([
      "select", "priority", "tracker_icon", "name", "progress", "status_icon", "state", "size", "dlspeed", "upspeed", "ratio",
    ])
  })

  it("puts several missing columns after state in reverse default order", () => {
    expect(readOrder(without(DEFAULT, "name", "size", "ratio"))).toEqual([
      "select", "priority", "tracker_icon", "progress", "status_icon", "state", "ratio", "size", "name", "dlspeed", "upspeed",
    ])
  })

  it("re-adds a missing select column after state, not first", () => {
    expect(readOrder(without(DEFAULT, "select"))).toEqual([
      "priority", "tracker_icon", "name", "size", "progress", "status_icon", "state", "select", "dlspeed", "upspeed", "ratio",
    ])
  })

  it.each([
    ["state", without(DEFAULT, "size", "state"), ["select", "priority", "tracker_icon", "name", "progress", "status_icon", "dlspeed", "upspeed", "ratio", "size", "state"]],
    ["dlspeed", without(DEFAULT, "size", "dlspeed"), ["select", "priority", "tracker_icon", "name", "progress", "status_icon", "state", "upspeed", "ratio", "size", "dlspeed"]],
  ])("appends missing columns in default order when %s is not saved", (_anchor, saved, expected) => {
    expect(readOrder(saved)).toEqual(expected)
  })

  it("puts a missing status_icon right after priority, not where the default has it", () => {
    expect(readOrder(without(DEFAULT, "status_icon"))).toEqual([
      "select", "priority", "status_icon", "tracker_icon", "name", "size", "progress", "state", "dlspeed", "upspeed", "ratio",
    ])
  })

  it("puts both missing icon columns after priority in reverse default order", () => {
    expect(readOrder(without(DEFAULT, "tracker_icon", "status_icon"))).toEqual([
      "select", "priority", "status_icon", "tracker_icon", "name", "size", "progress", "state", "dlspeed", "upspeed", "ratio",
    ])
  })

  it("appends a missing icon column when priority is in neither order", () => {
    const defaultOrder = without(DEFAULT, "priority")
    expect(readOrder(without(defaultOrder, "status_icon"), defaultOrder)).toEqual([
      "select", "tracker_icon", "name", "size", "progress", "state", "dlspeed", "upspeed", "ratio", "status_icon",
    ])
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
})
