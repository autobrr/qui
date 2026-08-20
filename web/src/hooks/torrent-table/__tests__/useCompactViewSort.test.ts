/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type UseCompactViewSortParams, useCompactViewSort } from "@/hooks/torrent-table/useCompactViewSort"
import type { Torrent } from "@/types"
import type { Table } from "@tanstack/react-table"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

type FakeColumn = { id: string; columnDef: { meta?: { headerString?: string }; header?: unknown } }

function fakeTable(columns: FakeColumn[]): Table<Torrent> {
  return { getAllLeafColumns: () => columns } as unknown as Table<Torrent>
}

const COLUMNS: FakeColumn[] = [
  { id: "name", columnDef: { header: "Name" } },
  { id: "size", columnDef: { header: "Size" } },
  { id: "status_icon", columnDef: { meta: { headerString: "Status" }, header: () => null } },
]

function render(overrides: Partial<UseCompactViewSortParams> = {}) {
  const params: UseCompactViewSortParams = {
    table: fakeTable(COLUMNS),
    columnVisibility: {},
    columnOrder: [],
    activeSortField: "name",
    activeSortOrder: "desc",
    setSorting: vi.fn(),
    setLastUserAction: vi.fn(),
    ...overrides,
  }
  return { params, ...renderHook(() => useCompactViewSort(params)) }
}

describe("useCompactViewSort", () => {
  it("filters sort options to the available columns (and their backend fields)", () => {
    const { result } = render()
    const values = result.current.compactSortOptions.map(o => o.value)
    expect(values).toContain("name")
    expect(values).toContain("size")
    // "status_icon" -> backend "state", which is a sort option value.
    expect(values).toContain("state")
    // A field with no matching column is excluded.
    expect(values).not.toContain("ratio")
  })

  it("labels the active field from its sort option", () => {
    const { result } = render({ activeSortField: "name" })
    expect(result.current.currentCompactSortLabel).toBe("Name")
  })

  it("falls back to a column's headerString when the field is not a sort option", () => {
    const { result } = render({ activeSortField: "status_icon" })
    expect(result.current.currentCompactSortLabel).toBe("Status")
  })

  it("changes the sort field and flags a user sort action", () => {
    const { params, result } = render({ activeSortField: "name" })
    act(() => result.current.handleCompactSortFieldChange("size"))
    // "size" is numeric, so getDefaultSortOrder("size") === "desc".
    expect(params.setSorting).toHaveBeenCalledWith([{ id: "size", desc: true }])
    expect(params.setLastUserAction).toHaveBeenCalledWith({ type: "sort", timestamp: expect.any(Number) })
  })

  it("ignores a no-op field change", () => {
    const { params, result } = render({ activeSortField: "size" })
    act(() => result.current.handleCompactSortFieldChange("size"))
    expect(params.setSorting).not.toHaveBeenCalled()
  })

  it("toggles the order (asc flips to desc)", () => {
    const { params, result } = render({ activeSortField: "name", activeSortOrder: "asc" })
    act(() => result.current.handleCompactSortOrderToggle())
    expect(params.setSorting).toHaveBeenCalledWith([{ id: "name", desc: true }])
  })

  it("toggles the order (desc flips to asc)", () => {
    const { params, result } = render({ activeSortField: "name", activeSortOrder: "desc" })
    act(() => result.current.handleCompactSortOrderToggle())
    expect(params.setSorting).toHaveBeenCalledWith([{ id: "name", desc: false }])
  })
})
