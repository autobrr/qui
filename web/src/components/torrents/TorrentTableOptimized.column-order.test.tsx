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

// These tests pin today's layout, quirks included, so a later change shows up as a diff.

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("getDefaultColumnOrder", () => {
  it("returns the fresh-user layout", () => {
    expect(getDefaultColumnOrder()).toEqual([
      "select", "priority", "tracker_icon", "name", "size", "total_size", "progress", "status_icon", "state",
      "num_seeds", "num_leechs", "dlspeed", "upspeed", "eta", "ratio", "popularity", "category", "tags",
      "added_on", "completion_on", "tracker", "dl_limit", "up_limit", "downloaded", "uploaded",
      "downloaded_session", "uploaded_session", "amount_left", "time_active", "seeding_time", "save_path",
      "completed", "ratio_limit", "seen_complete", "last_activity", "availability", "infohash_v1",
      "infohash_v2", "reannounce", "private",
    ])
  })

  it("places status_icon differently for a fresh user and for a saved order that lacks it", () => {
    const defaultOrder = getDefaultColumnOrder()
    window.localStorage.setItem("qui-column-order:1", JSON.stringify(defaultOrder.filter(id => id !== "status_icon")))

    const { result } = renderHook(() => usePersistedColumnOrder(defaultOrder, 1))

    expect(defaultOrder.slice(6, 9)).toEqual(["progress", "status_icon", "state"])
    expect(result.current[0].slice(0, 4)).toEqual(["select", "priority", "status_icon", "tracker_icon"])
  })

  it("leaves instance out, so the unified view shows it last instead of after name", () => {
    const columns = createColumns(false, undefined, "bytes", undefined, undefined, undefined, true, true)
    const defaultOrder = getDefaultColumnOrder()

    const { result } = renderHook(() => useTable({
      features: torrentTableFeatures,
      data: [],
      columns,
      state: { columnOrder: defaultOrder },
    }))
    const definitionIds = columns.map(col => col.id ?? (col as { accessorKey?: string }).accessorKey)
    const headerIds = result.current.getHeaderGroups()[0].headers.map(header => header.column.id)

    expect(definitionIds.slice(2, 5)).toEqual(["name", "instance", "size"])
    expect(defaultOrder).not.toContain("instance")
    expect(headerIds).toEqual([...defaultOrder, "instance"])
  })
})
