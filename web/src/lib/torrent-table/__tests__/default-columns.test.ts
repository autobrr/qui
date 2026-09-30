/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { createColumns } from "@/components/torrents/TorrentTableColumns"
import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { DEFAULT_COLUMN_ORDER, DEFAULT_COLUMN_VISIBILITY, DEFAULT_UNIFIED_COLUMN_ORDER } from "@/lib/torrent-table/default-columns"

function columnIds(isUnifiedView: boolean): string[] {
  return createColumns(false, undefined, "bytes", undefined, undefined, undefined, true, isUnifiedView)
    .map(col => col.id ?? (col as { accessorKey?: string }).accessorKey ?? "")
}

// Names the offending ids, so a new column left out of the defaults fails with its id.
function compareIds(listed: string[], columns: string[]) {
  return {
    missing: columns.filter(id => !listed.includes(id)),
    extra: listed.filter(id => !columns.includes(id)),
    duplicated: listed.filter((id, index) => listed.indexOf(id) !== index),
  }
}

const NO_DIFFERENCE = { missing: [], extra: [], duplicated: [] }

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

describe("default columns", () => {
  // A column missing from the default order renders last and is never added to saved orders.
  it.each([
    ["an instance view", false, DEFAULT_COLUMN_ORDER],
    ["the unified view", true, DEFAULT_UNIFIED_COLUMN_ORDER],
  ])("lists every column of %s exactly once", (_view, isUnifiedView, defaultOrder) => {
    expect(compareIds(defaultOrder, columnIds(isUnifiedView))).toEqual(NO_DIFFERENCE)
  })

  it("gives every column a default visibility", () => {
    expect(compareIds(Object.keys(DEFAULT_COLUMN_VISIBILITY), columnIds(true))).toEqual(NO_DIFFERENCE)
  })

  it("gives a saved order that lacks status_icon the fresh-user slot", () => {
    window.localStorage.setItem("qui-column-order:1", JSON.stringify(DEFAULT_COLUMN_ORDER.filter(id => id !== "status_icon")))

    const { result } = renderHook(() => usePersistedColumnOrder(DEFAULT_COLUMN_ORDER, 1))

    expect(result.current[0]).toEqual(DEFAULT_COLUMN_ORDER)
  })

  // Unified-view orders saved before instance joined the default have no instance id.
  it("adds instance after name to a unified-view order saved without it", () => {
    window.localStorage.setItem("qui-column-order:0", JSON.stringify(DEFAULT_COLUMN_ORDER))

    const { result } = renderHook(() => usePersistedColumnOrder(DEFAULT_UNIFIED_COLUMN_ORDER, 0))

    expect(result.current[0]).toEqual(DEFAULT_UNIFIED_COLUMN_ORDER)
    expect(result.current[0].slice(3, 5)).toEqual(["name", "instance"])
  })
})
