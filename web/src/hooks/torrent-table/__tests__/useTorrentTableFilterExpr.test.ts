/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentTableFilterExpr } from "@/hooks/torrent-table/useTorrentTableFilterExpr"
import type { ColumnFilter } from "@/lib/column-filter-utils"
import { makeFilters } from "@/test/mockFilters"
import type { TorrentFilters } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

// Router search is the only external dependency that needs a provider; stub it.
const hoisted = vi.hoisted(() => ({ routeSearch: {} as { q?: string } }))
vi.mock("@tanstack/react-router", () => ({
  useSearch: () => hoisted.routeSearch,
}))

type Props = { filters?: TorrentFilters; instanceId: number; columnFilters: ColumnFilter[] }

// A deterministic single column filter (-> "AddedOn > 1704067200").
const ADDED_ON_FILTER: ColumnFilter = { columnId: "added_on", operation: "gt", value: "2024-01-01" }

function render(initialProps: Props) {
  return renderHook((props: Props) => useTorrentTableFilterExpr(props), { initialProps })
}

beforeEach(() => {
  hoisted.routeSearch = {}
})

describe("useTorrentTableFilterExpr — search derivation", () => {
  it("prefers the trimmed route search over the debounced input", () => {
    hoisted.routeSearch = { q: "  fromRoute  " }
    const { result } = render({ instanceId: 1, columnFilters: [] })
    expect(result.current.effectiveSearch).toBe("fromRoute")
  })

  it("debounces the global filter into effectiveSearch", () => {
    vi.useFakeTimers()
    try {
      const { result } = render({ instanceId: 1, columnFilters: [] })
      expect(result.current.effectiveSearch).toBe("")

      act(() => result.current.setGlobalFilter("linux"))
      // Not yet applied — debounce window hasn't elapsed.
      expect(result.current.effectiveSearch).toBe("")

      act(() => vi.advanceTimersByTime(200))
      expect(result.current.effectiveSearch).toBe("linux")
    } finally {
      vi.useRealTimers()
    }
  })
})

describe("useTorrentTableFilterExpr — columnFiltersExpr", () => {
  it("is null when there are no column filters", () => {
    const { result } = render({ instanceId: 1, columnFilters: [] })
    expect(result.current.columnFiltersExpr).toBeNull()
  })

  it("converts column filters to a backend expression", () => {
    const { result } = render({ instanceId: 1, columnFilters: [ADDED_ON_FILTER] })
    expect(result.current.columnFiltersExpr).toBe("AddedOn > 1704067200")
  })
})

describe("useTorrentTableFilterExpr — isDoingCrossSeedFiltering", () => {
  it("is true only when the expr contains both a Hash equality and an OR", () => {
    const { result } = render({
      instanceId: 1,
      columnFilters: [],
      filters: makeFilters({ expr: "Hash == \"abc\" || Hash == \"def\"" }),
    })
    expect(result.current.isDoingCrossSeedFiltering).toBe(true)
  })

  it("is falsy without the OR", () => {
    const { result } = render({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "Hash == \"abc\"" }) })
    expect(result.current.isDoingCrossSeedFiltering).toBeFalsy()
  })

  it("is falsy without a Hash equality", () => {
    const { result } = render({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "state == \"x\" || state == \"y\"" }) })
    expect(result.current.isDoingCrossSeedFiltering).toBeFalsy()
  })
})

describe("useTorrentTableFilterExpr — combinedFiltersExpr (#1925 cross-seed early-return)", () => {
  it("drops column filters from the backend expression in cross-seed mode", () => {
    // Regression #1925: in cross-seed mode the column filters are applied
    // client-side, so combinedFiltersExpr must return ONLY filters.expr.
    const crossSeedExpr = "Hash == \"abc\" || Hash == \"def\""
    const { result } = render({
      instanceId: 1,
      columnFilters: [ADDED_ON_FILTER],
      filters: makeFilters({ expr: crossSeedExpr }),
    })
    // Sanity: the column filter really does produce an expression...
    expect(result.current.columnFiltersExpr).toBe("AddedOn > 1704067200")
    // ...but it must NOT be folded into the backend expression here.
    expect(result.current.combinedFiltersExpr).toBe(crossSeedExpr)
  })

  it("combines column filters and filters.expr with && when not cross-seeding", () => {
    const { result } = render({
      instanceId: 1,
      columnFilters: [ADDED_ON_FILTER],
      filters: makeFilters({ expr: "state == \"downloading\"" }),
    })
    expect(result.current.combinedFiltersExpr).toBe("(AddedOn > 1704067200) && (state == \"downloading\")")
  })

  it("returns the column expression alone when there is no filters.expr", () => {
    const { result } = render({ instanceId: 1, columnFilters: [ADDED_ON_FILTER] })
    expect(result.current.combinedFiltersExpr).toBe("AddedOn > 1704067200")
  })

  it("returns filters.expr alone when there are no column filters", () => {
    const { result } = render({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "state == \"x\"" }) })
    expect(result.current.combinedFiltersExpr).toBe("state == \"x\"")
  })

  it("is undefined when neither column filters nor filters.expr are present", () => {
    const { result } = render({ instanceId: 1, columnFilters: [] })
    expect(result.current.combinedFiltersExpr).toBeUndefined()
  })
})

describe("useTorrentTableFilterExpr — lastUserAction", () => {
  it("does not fire on mount with no active filters/search", () => {
    const { result } = render({ instanceId: 1, columnFilters: [] })
    expect(result.current.lastUserAction).toBeNull()
  })

  it("does not fire a spurious search action when mounting with a route search query", () => {
    // Regression: a URL with ?q= seeds effectiveSearch on mount, which must not
    // be treated as a user-initiated search.
    hoisted.routeSearch = { q: "linux" }
    const { result } = render({ instanceId: 1, columnFilters: [] })
    expect(result.current.effectiveSearch).toBe("linux")
    expect(result.current.lastUserAction).toBeNull()
  })

  it("fires a filter action when filters change", () => {
    const { result, rerender } = render({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "a" }) })
    rerender({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "b" }) })
    expect(result.current.lastUserAction?.type).toBe("filter")
  })

  it("fires an instance action when the instance changes", () => {
    const { result, rerender } = render({ instanceId: 1, columnFilters: [] })
    rerender({ instanceId: 2, columnFilters: [] })
    expect(result.current.lastUserAction?.type).toBe("instance")
  })

  it("does not fire when a new filters object has identical contents", () => {
    const { result, rerender } = render({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "a" }) })
    // New object reference, same content — JSON comparison should treat it as unchanged.
    rerender({ instanceId: 1, columnFilters: [], filters: makeFilters({ expr: "a" }) })
    expect(result.current.lastUserAction).toBeNull()
  })
})
