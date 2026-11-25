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

const t = i18n.getFixedT("en", "search")

describe("getCategoriesForSearchType", () => {
  it.each<[Parameters<typeof getCategoriesForSearchType>[0], number]>([
    ["movies", 2000],
    ["tv", 5000],
    ["music", 3000],
    ["books", 7000],
    ["apps", 4000],
    ["xxx", 6000],
  ])("maps %s to parent ID %i in both directions", (type, parent) => {
    const cats = getCategoriesForSearchType(type)
    expect(cats[0]).toBe(parent)
    expect(inferSearchTypeFromCategories(cats)).toBe(type)
  })

  it("returns a fresh array on each call (callers can mutate safely)", () => {
    const a = getCategoriesForSearchType("movies")
    const b = getCategoriesForSearchType("movies")
    expect(a).not.toBe(b)
    a.push(9999)
    expect(getCategoriesForSearchType("movies")).not.toContain(9999)
  })
})

describe("inferSearchTypeFromCategories", () => {
  it("returns null for empty / missing categories", () => {
    expect(inferSearchTypeFromCategories(undefined)).toBeNull()
    expect(inferSearchTypeFromCategories([])).toBeNull()
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

describe("getSearchTypeLabel", () => {
  it.each([
    ["movies", "Movies"],
    ["tv", "TV"],
    ["music", "Music"],
    ["books", "Books & comics"],
    ["apps", "Apps & games"],
    ["xxx", "Adult"],
  ] as const)("labels %s as %s", (type, expected) => {
    expect(getSearchTypeLabel(type, t)).toBe(expected)
  })

})

describe("getSearchTypeOptions", () => {
  it("does not offer automatic category detection", () => {
    expect(getSearchTypeOptions(t).map(o => o.value).sort()).toEqual(
      ["apps", "books", "movies", "music", "tv", "xxx"]
    )
  })
})
