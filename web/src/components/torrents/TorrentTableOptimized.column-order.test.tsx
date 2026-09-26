/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, renderHook } from "@testing-library/react"
import { useTable } from "@tanstack/react-table"
import { afterEach, describe, expect, it } from "vitest"

import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { torrentTableFeatures } from "./tanstackTableFeatures"
import { createColumns } from "./TorrentTableColumns"
import { getDefaultColumnOrder } from "./TorrentTableOptimized"

const INSTANCE_ORDER = [
  "select", "priority", "tracker_icon", "name", "size", "total_size", "progress", "status_icon", "state",
  "num_seeds", "num_leechs", "dlspeed", "upspeed", "eta", "ratio", "popularity", "category", "tags",
  "added_on", "completion_on", "tracker", "dl_limit", "up_limit", "downloaded", "uploaded",
  "downloaded_session", "uploaded_session", "amount_left", "time_active", "seeding_time", "save_path",
  "completed", "ratio_limit", "seen_complete", "last_activity", "availability", "infohash_v1",
  "infohash_v2", "reannounce", "private",
]

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("getDefaultColumnOrder", () => {
  it("returns the fresh-user layout of an instance view", () => {
    expect(getDefaultColumnOrder(false)).toEqual(INSTANCE_ORDER)
  })

  it("puts instance right after name in the unified view", () => {
    const expected = [...INSTANCE_ORDER]
    expected.splice(expected.indexOf("name") + 1, 0, "instance")

    expect(getDefaultColumnOrder(true)).toEqual(expected)
  })

  it("gives a saved order that lacks status_icon the fresh-user slot", () => {
    const defaultOrder = getDefaultColumnOrder(false)
    window.localStorage.setItem("qui-column-order:1", JSON.stringify(defaultOrder.filter(id => id !== "status_icon")))

    const { result } = renderHook(() => usePersistedColumnOrder(defaultOrder, 1))

    expect(result.current[0]).toEqual(defaultOrder)
  })

  // Unified-view orders saved before instance joined the default have no instance id.
  it("adds instance after name to a unified-view order saved without it", () => {
    window.localStorage.setItem("qui-column-order:0", JSON.stringify(INSTANCE_ORDER))

    const { result } = renderHook(() => usePersistedColumnOrder(getDefaultColumnOrder(true), 0))

    expect(result.current[0]).toEqual(getDefaultColumnOrder(true))
  })

  it("renders the unified-view header in the default order", () => {
    const defaultOrder = getDefaultColumnOrder(true)
    const columns = createColumns(false, undefined, "bytes", undefined, undefined, undefined, true, true)

    const { result } = renderHook(() => useTable({
      features: torrentTableFeatures,
      data: [],
      columns,
      state: { columnOrder: defaultOrder },
    }))

    expect(result.current.getHeaderGroups()[0].headers.map(header => header.column.id)).toEqual(defaultOrder)
  })
})
