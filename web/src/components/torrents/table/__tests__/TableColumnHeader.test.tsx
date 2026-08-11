/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TableColumnHeader, type TableColumnHeaderProps } from "@/components/torrents/table/TableColumnHeader"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import type { Table } from "@tanstack/react-table"
import type { Torrent } from "@/types"
import { cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Capture each column's onFilterChange so we can drive it directly (the real
// DraggableTableHeader's sortable/popover internals are irrelevant to this test).
const captured: Record<string, (columnId: string, filter: ColumnFilter | null) => void> = {}
vi.mock("@/components/torrents/DraggableTableHeader", () => ({
  DraggableTableHeader: ({ header, onFilterChange }: { header: { column: { id: string } }; onFilterChange?: (id: string, f: ColumnFilter | null) => void }) => {
    if (onFilterChange) captured[header.column.id] = onFilterChange
    return <div data-testid={`hdr-${header.column.id}`} />
  },
}))

function fakeTable(columnIds: string[]): Table<Torrent> {
  return {
    getHeaderGroups: () => [{
      id: "hg-0",
      headers: columnIds.map(id => ({ id: `h-${id}`, column: { id } })),
    }],
  } as unknown as Table<Torrent>
}

function filter(columnId: string, value: string): ColumnFilter {
  return { columnId, value } as unknown as ColumnFilter
}

function makeProps(over: Partial<TableColumnHeaderProps> = {}): TableColumnHeaderProps {
  return {
    table: fakeTable(["name", "size"]),
    sensors: [],
    onDragEnd: vi.fn(),
    columnFilters: [],
    setColumnFilters: vi.fn(),
    minTableWidth: 600,
    viewMode: "normal",
    ...over,
  }
}

beforeEach(() => { for (const k of Object.keys(captured)) delete captured[k] })
afterEach(cleanup)

describe("TableColumnHeader", () => {
  it("renders nothing in compact view", () => {
    const { container, queryByTestId } = render(<TableColumnHeader {...makeProps({ viewMode: "compact" })} />)
    expect(container.firstChild).toBeNull()
    expect(queryByTestId("hdr-name")).toBeNull()
  })

  it("renders a draggable header per column otherwise", () => {
    const { getByTestId } = render(<TableColumnHeader {...makeProps()} />)
    expect(getByTestId("hdr-name")).not.toBeNull()
    expect(getByTestId("hdr-size")).not.toBeNull()
  })

  it("removes a column filter when onFilterChange is called with null", () => {
    const setColumnFilters = vi.fn()
    const existing = [filter("name", "x"), filter("size", "y")]
    render(<TableColumnHeader {...makeProps({ columnFilters: existing, setColumnFilters })} />)

    captured["name"]("name", null)

    expect(setColumnFilters).toHaveBeenCalledWith([existing[1]])
  })

  it("appends a new column filter", () => {
    const setColumnFilters = vi.fn()
    const existing = [filter("name", "x")]
    render(<TableColumnHeader {...makeProps({ columnFilters: existing, setColumnFilters })} />)

    const added = filter("size", "y")
    captured["size"]("size", added)

    expect(setColumnFilters).toHaveBeenCalledWith([existing[0], added])
  })

  it("replaces an existing column filter in place", () => {
    const setColumnFilters = vi.fn()
    const existing = [filter("name", "x"), filter("size", "y")]
    render(<TableColumnHeader {...makeProps({ columnFilters: existing, setColumnFilters })} />)

    const replacement = filter("name", "z")
    captured["name"]("name", replacement)

    expect(setColumnFilters).toHaveBeenCalledWith([replacement, existing[1]])
  })
})
