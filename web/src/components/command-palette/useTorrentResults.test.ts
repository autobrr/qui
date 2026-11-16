/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"
import { EMPTY_SIDEBAR_FILTERS } from "@/lib/filter-views"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { describe, expect, it } from "vitest"
import { type ListView, listSearchScope } from "./useTorrentResults"

const RATIO_ABOVE_TWO: ColumnFilter[] = [{ columnId: "ratio", operation: "gt", value: "2" }]
const CROSS_SEEDS = "Hash == \"aa\" || Hash == \"bb\""

const view = (changes: Partial<ListView> = {}): ListView => ({
  list: 3,
  sidebarFilters: EMPTY_SIDEBAR_FILTERS,
  columnFilters: [],
  phoneLayout: false,
  unifiedInstanceIds: [1, 2, 3],
  ...changes,
})

describe("listSearchScope", () => {
  it("asks the instance of a list without filters, with copy groups", () => {
    const scope = listSearchScope(view())

    expect(scope).toEqual({ filters: { ...EMPTY_SIDEBAR_FILTERS, expr: undefined }, filtered: false, instanceIds: [3], groupCopies: true })
  })

  it("sends the sidebar filters of the list, with the column filters in the expression", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, status: ["downloading"], expr: "Size > 5" }

    const scope = listSearchScope(view({ sidebarFilters, columnFilters: RATIO_ABOVE_TWO }))

    expect(scope.filters).toEqual({ ...sidebarFilters, expr: "(Ratio > 2) && (Size > 5)" })
    expect(scope.filtered).toBe(true)
  })

  it("counts a column filter alone as a filter", () => {
    const scope = listSearchScope(view({ columnFilters: RATIO_ABOVE_TWO }))

    expect([scope.filters.expr, scope.filtered]).toEqual(["Ratio > 2", true])
  })

  it("leaves the column filters out on the phone layout, which has none", () => {
    const scope = listSearchScope(view({ columnFilters: RATIO_ABOVE_TWO, phoneLayout: true }))

    expect([scope.filters.expr, scope.filtered]).toEqual([undefined, false])
  })

  it("asks the instances of the Unified view", () => {
    expect(listSearchScope(view({ list: ALL_INSTANCES_ID })).instanceIds).toEqual([1, 2, 3])
  })

  it("asks each instance for the copies in Filter Cross-Seeds, one row for each copy", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, expr: CROSS_SEEDS }

    const scope = listSearchScope(view({ sidebarFilters }))

    expect(scope).toEqual({ filters: sidebarFilters, filtered: true, instanceIds: undefined, groupCopies: false })
  })

  it("sends the column filters in Filter Cross-Seeds too, where the table applies them in the browser", () => {
    const sidebarFilters = { ...EMPTY_SIDEBAR_FILTERS, expr: CROSS_SEEDS }

    const scope = listSearchScope(view({ sidebarFilters, columnFilters: RATIO_ABOVE_TWO }))

    expect(scope.filters.expr).toBe(`(Ratio > 2) && (${CROSS_SEEDS})`)
  })
})
