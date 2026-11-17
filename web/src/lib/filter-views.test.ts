/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { EMPTY_SIDEBAR_FILTERS, filterViewsEqual, hasSidebarFilters, toViewFilters } from "./filter-views"
import { makeFilters } from "@/test/mockFilters"

describe("toViewFilters", () => {
  it("drops expanded fields and fills missing keys", () => {
    const view = toViewFilters({
      categories: ["movies"],
      expandedCategories: ["movies", "movies/4k"],
      expandedExcludeCategories: ["tv"],
    })

    expect(view).toEqual(makeFilters({ categories: ["movies"], expr: "" }))
    expect("expandedCategories" in view).toBe(false)
    expect("expandedExcludeCategories" in view).toBe(false)
  })

  it("normalizes a missing expr to an empty string", () => {
    expect(toViewFilters(undefined).expr).toBe("")
  })
})

describe("filterViewsEqual", () => {
  it("ignores array order and expanded fields", () => {
    expect(filterViewsEqual(
      { tags: ["b", "a"], categories: ["movies"], expandedCategories: ["movies/4k"] },
      { tags: ["a", "b"], categories: ["movies"] }
    )).toBe(true)
  })

  it("treats an absent expr as empty", () => {
    expect(filterViewsEqual({ expr: "" }, {})).toBe(true)
  })

  it("separates different selections", () => {
    expect(filterViewsEqual({ status: ["downloading"] }, { status: ["seeding"] })).toBe(false)
    expect(filterViewsEqual({ status: ["downloading"] }, { excludeStatus: ["downloading"] })).toBe(false)
    expect(filterViewsEqual({ expr: "size > 1" }, {})).toBe(false)
  })
})

describe("hasSidebarFilters", () => {
  it.each([
    ["status", { status: ["unregistered"] }],
    ["excludeStatus", { excludeStatus: ["seeding"] }],
    ["categories", { categories: ["movies"] }],
    ["excludeCategories", { excludeCategories: ["tv"] }],
    ["tags", { tags: ["keep"] }],
    ["excludeTags", { excludeTags: ["keep"] }],
    ["trackers", { trackers: ["tracker.example.invalid"] }],
    ["excludeTrackers", { excludeTrackers: ["tracker.example.invalid"] }],
    ["expr", { expr: "Ratio < 1" }],
  ])("reports a set %s filter", (_, overrides) => {
    expect(hasSidebarFilters(makeFilters(overrides))).toBe(true)
  })

  it("reports no filter for an empty or blank selection", () => {
    expect(hasSidebarFilters(makeFilters())).toBe(false)
    expect(hasSidebarFilters(makeFilters({ expr: "  " }))).toBe(false)
  })

  it("reports no filter after a clear", () => {
    expect(hasSidebarFilters(EMPTY_SIDEBAR_FILTERS)).toBe(false)
  })
})
