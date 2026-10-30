/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableFilterExpr } from "@/hooks/torrent-table/useTorrentTableFilterExpr"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import { makeFilters } from "@/test/mockFilters"
import type { TorrentFilters } from "@/types"
import { cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Router search is the only external dependency that needs a provider; stub it.
const hoisted = vi.hoisted(() => ({ routeSearch: {} as { q?: string } }))
vi.mock("@tanstack/react-router", () => ({
  useSearch: () => hoisted.routeSearch,
}))

type Props = { filters?: TorrentFilters; columnFilters: ColumnFilter[] }

// A deterministic single column filter (-> "AddedOn > 1704067200").
const ADDED_ON_FILTER: ColumnFilter = { columnId: "added_on", operation: "gt", value: "2024-01-01" }

function render(initialProps: Props) {
  return renderHook((props: Props) => useTorrentTableFilterExpr(props), { initialProps })
}

beforeEach(() => {
  hoisted.routeSearch = {}
})

afterEach(cleanup)

describe("useTorrentTableFilterExpr — search derivation", () => {
  it("trims the route search into effectiveSearch", () => {
    hoisted.routeSearch = { q: "  fromRoute  " }
    const { result } = render({ columnFilters: [] })
    expect(result.current.effectiveSearch).toBe("fromRoute")
  })

  it("is empty without a route search", () => {
    const { result } = render({ columnFilters: [] })
    expect(result.current.effectiveSearch).toBe("")
  })
})

describe("useTorrentTableFilterExpr — columnFiltersExpr", () => {
  it("is null when there are no column filters", () => {
    const { result } = render({ columnFilters: [] })
    expect(result.current.columnFiltersExpr).toBeNull()
  })

  it("converts column filters to a backend expression", () => {
    const { result } = render({ columnFilters: [ADDED_ON_FILTER] })
    expect(result.current.columnFiltersExpr).toBe("AddedOn > 1704067200")
  })
})

describe("useTorrentTableFilterExpr — isDoingCrossSeedFiltering", () => {
  it("is true only when the expr contains both a Hash equality and an OR", () => {
    const { result } = render({
      columnFilters: [],
      filters: makeFilters({ expr: "Hash == \"abc\" || Hash == \"def\"" }),
    })
    expect(result.current.isDoingCrossSeedFiltering).toBe(true)
  })

  it("is falsy without the OR", () => {
    const { result } = render({ columnFilters: [], filters: makeFilters({ expr: "Hash == \"abc\"" }) })
    expect(result.current.isDoingCrossSeedFiltering).toBeFalsy()
  })

  it("is falsy without a Hash equality", () => {
    const { result } = render({ columnFilters: [], filters: makeFilters({ expr: "state == \"x\" || state == \"y\"" }) })
    expect(result.current.isDoingCrossSeedFiltering).toBeFalsy()
  })
})
