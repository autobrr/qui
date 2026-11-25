/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { buildSearchRequest, facetOptions, filterResults, sortResults, tickResult } from "@/lib/search-results"
import type { TorznabSearchResult } from "@/types"
import { describe, expect, it } from "vitest"

function result(overrides: Partial<TorznabSearchResult>): TorznabSearchResult {
  return {
    indexer: "Aurora",
    indexerId: 1,
    title: "Sample.Film.2023.1080p.WEB-DL-RIG",
    downloadUrl: "http://127.0.0.1/dl/1.torrent",
    size: 1024,
    seeders: 10,
    leechers: 1,
    categoryId: 2000,
    categoryName: "Movies",
    publishDate: "2026-11-01T00:00:00Z",
    downloadVolumeFactor: 1,
    uploadVolumeFactor: 1,
    guid: "g-1",
    ...overrides,
  }
}

const categoryName = (r: TorznabSearchResult) => r.categoryName ?? ""

describe("buildSearchRequest", () => {
  it("sends only the fields of the search type, and drops bad numbers and fractions", () => {
    const params = { season: "2", episode: "x", year: "2020", imdbId: " tt0000001 " }
    const tv = buildSearchRequest({ query: "Sample Show", type: "tv", indexerIds: [1, 2], params })
    expect(tv.season).toBe(2)
    expect(tv.episode).toBeUndefined()
    expect(tv.year).toBeUndefined()
    expect(tv.imdb_id).toBe("tt0000001")
    expect(tv.indexer_ids).toEqual([1, 2])
    expect("cache_mode" in tv).toBe(false)

    const movies = buildSearchRequest({ query: "Sample Film", type: "movies", indexerIds: [1], params, bypassCache: true })
    expect(movies.season).toBeUndefined()
    expect(movies.year).toBe(2020)
    expect(movies.cache_mode).toBe("bypass")

    const fractions = buildSearchRequest({ query: "Sample Show", type: "tv", indexerIds: [1], params: { season: "1.5", episode: "3", limit: "2.5" } })
    expect(fractions.season).toBeUndefined()
    expect(fractions.episode).toBe(3)
    expect(fractions.limit).toBeUndefined()
  })

  it("reads an IMDb ID from the query when the field is empty", () => {
    const request = buildSearchRequest({ query: "Sample Film tt7654321", type: "movies", indexerIds: [1], params: {} })
    expect(request.imdb_id).toBe("tt7654321")
  })
})

describe("filterResults", () => {
  const results = [
    result({ guid: "a", title: "Sample.Film.2023.2160p.BluRay-RIG", indexer: "Aurora", group: "RIG", downloadVolumeFactor: 0 }),
    result({ guid: "b", title: "Sample.Film.2023.1080p.WEB-DL-OTHER", indexer: "BitHaven", group: "OTHER" }),
    result({ guid: "c", title: "Another.Film.2023.2160p.WEB-DL-RIG", indexer: "BitHaven", group: "RIG" }),
  ]

  it("needs every word of the text in any field, and a value from each selected facet", () => {
    expect(filterResults(results, "sample bithaven", {}, categoryName).map(r => r.guid)).toEqual(["b"])
    expect(filterResults(results, "", { group: ["RIG"], indexer: ["BitHaven"] }, categoryName).map(r => r.guid)).toEqual(["c"])
    expect(filterResults(results, "2160p", { freeleech: ["0"] }, categoryName).map(r => r.guid)).toEqual(["a"])
    expect(filterResults([result({ guid: "d" })], "", { group: ["RIG"] }, categoryName)).toEqual([])
  })
})

describe("facetOptions", () => {
  it("counts each value, skips missing ones, and puts the largest first, but orders freeleech from free to full price", () => {
    const groups = [result({ group: "B" }), result({ group: "A" }), result({ group: "B" }), result({})]
    expect(facetOptions(groups, "group", categoryName)).toEqual([{ value: "B", count: 2 }, { value: "A", count: 1 }])
    const discounts = [result({ downloadVolumeFactor: 1 }), result({ downloadVolumeFactor: 0.5 }), result({ downloadVolumeFactor: 0 })]
    expect(facetOptions(discounts, "freeleech", categoryName).map(o => o.value)).toEqual(["0", "0.5", "1"])
  })
})

describe("sortResults", () => {
  it("sorts in either direction and breaks ties on seeders", () => {
    const results = [
      result({ guid: "a", size: 2, seeders: 1 }),
      result({ guid: "b", size: 1, seeders: 5 }),
      result({ guid: "c", size: 2, seeders: 9 }),
    ]
    expect(sortResults(results, { key: "size", desc: true }).map(r => r.guid)).toEqual(["c", "a", "b"])
    expect(sortResults(results, { key: "size", desc: false }).map(r => r.guid)).toEqual(["b", "c", "a"])
  })

  it("sorts titles in natural order", () => {
    const results = [result({ guid: "a", title: "Show S10" }), result({ guid: "b", title: "show S9" })]
    expect(sortResults(results, { key: "title", desc: false }).map(r => r.guid)).toEqual(["b", "a"])
  })
})

describe("tickResult", () => {
  const keys = ["a", "b", "c", "d"]

  it("ticks one row, or the rows from the last tick when Shift is down, and one row when the last ticked row is gone", () => {
    expect([...tickResult(new Set(), keys, "b", null)]).toEqual(["b"])
    expect([...tickResult(new Set(["b"]), keys, "d", "b")].sort()).toEqual(["b", "c", "d"])
    expect([...tickResult(new Set(["a", "b", "c"]), keys, "a", "c")]).toEqual([])
    expect([...tickResult(new Set(["x"]), keys, "c", "x")].sort()).toEqual(["c", "x"])
  })

  it("changes nothing for a row that left the page, also with Shift down after a tick on the last row", () => {
    expect([...tickResult(new Set(["b"]), keys, "gone", "d")]).toEqual(["b"])
  })
})
