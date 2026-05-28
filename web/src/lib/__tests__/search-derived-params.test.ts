/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"
import {
  getCategoriesForSearchType,
  getSearchTypeLabel,
  getSearchTypeOptions,
  inferSearchTypeFromCategories
} from "@/lib/search-derived-params"
import { describe, expect, it } from "vitest"

// Label helpers now resolve through i18n; bind a fixed English translator
// scoped to the search namespace so assertions stay deterministic.
const t = i18n.getFixedT("en", "search")

// Intent: SearchType -> Torznab category IDs. 'auto' means "don't constrain"
// (returns undefined so the backend chooses). Returns a NEW array so callers
// can mutate safely without corrupting the module-level map.
describe("getCategoriesForSearchType", () => {
  it("returns undefined for 'auto'", () => {
    expect(getCategoriesForSearchType("auto")).toBeUndefined()
  })

  it.each<[Exclude<Parameters<typeof getCategoriesForSearchType>[0], "auto">, number]>([
    ["movies", 2000],
    ["tv", 5000],
    ["music", 3000],
    ["books", 7000],
    ["apps", 4000],
    ["xxx", 6000],
  ])("returns a category list starting at the parent ID for %s", (type, parent) => {
    const cats = getCategoriesForSearchType(type)
    expect(cats).toBeDefined()
    expect(cats?.[0]).toBe(parent)
  })

  it("returns a fresh array on each call (callers can mutate safely)", () => {
    const a = getCategoriesForSearchType("movies")!
    const b = getCategoriesForSearchType("movies")!
    expect(a).not.toBe(b)
    a.push(9999)
    expect(getCategoriesForSearchType("movies")).not.toContain(9999)
  })
})

// Intent: collapse a category-id list back into a SearchType when every
// category falls under the same parent family. Returning null whenever
// families mix protects against silently mislabeling a mixed-category
// search (e.g. user manually selected one Movies + one TV id).
describe("inferSearchTypeFromCategories", () => {
  it("returns null for empty / missing categories", () => {
    expect(inferSearchTypeFromCategories(undefined)).toBeNull()
    expect(inferSearchTypeFromCategories([])).toBeNull()
  })

  it.each([
    [[2000, 2010, 2020], "movies"],
    [[5000, 5040], "tv"],
    [[3000], "music"],
    [[7020], "books"],
    [[4000], "apps"],
    [[6060], "xxx"],
  ] as const)("infers %s from %j", (cats, expected) => {
    expect(inferSearchTypeFromCategories([...cats])).toBe(expected)
  })

  it("returns null when categories cross family boundaries", () => {
    expect(inferSearchTypeFromCategories([2000, 5000])).toBeNull()
    expect(inferSearchTypeFromCategories([2010, 3000])).toBeNull()
  })

  it("returns null when the parent category isn't a known family root", () => {
    expect(inferSearchTypeFromCategories([8000])).toBeNull()
    expect(inferSearchTypeFromCategories([1000, 2000])).toBeNull()
  })
})

// Intent: human-readable label for the SearchType dropdown. Falls back to
// 'Auto detect' for unknown values rather than blowing up the UI.
describe("getSearchTypeLabel", () => {
  it.each([
    ["auto", "Auto detect"],
    ["movies", "Movies"],
    ["tv", "TV"],
    ["music", "Music"],
    ["books", "Books & comics"],
    ["apps", "Apps & games"],
    ["xxx", "Adult"],
  ] as const)("labels %s as %s", (type, expected) => {
    expect(getSearchTypeLabel(type, t)).toBe(expected)
  })

  it("falls back to 'Auto detect' for unknown values", () => {
    expect(getSearchTypeLabel("unknown" as never, t)).toBe("Auto detect")
  })
})

// Intent: each option in the dropdown must be a known SearchType so the
// types stay in sync with the data shape. Catches anyone who adds an
// option label without adding the corresponding type entry.
describe("getSearchTypeOptions", () => {
  it("includes exactly the known SearchType values", () => {
    expect(getSearchTypeOptions(t).map(o => o.value).sort()).toEqual(
      ["apps", "auto", "books", "movies", "music", "tv", "xxx"]
    )
  })
})
