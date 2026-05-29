/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { mergeStreamedFirstPage } from "@/lib/stream-merge"

type Row = { hash: string }
const rows = (...hashes: string[]): Row[] => hashes.map(hash => ({ hash }))
const hashes = (list: Row[]) => list.map(row => row.hash)

describe("mergeStreamedFirstPage", () => {
  it("returns the fresh page when there is no prior list", () => {
    expect(mergeStreamedFirstPage([], rows("a", "b"))).toEqual(rows("a", "b"))
  })

  it("returns empty when the fresh page is empty", () => {
    expect(mergeStreamedFirstPage(rows("a"), [])).toEqual([])
  })

  it("replaces the page-0 window without duplicating on a steady update", () => {
    expect(mergeStreamedFirstPage(rows("a", "b", "c"), rows("a", "b", "c"), 3)).toEqual(
      rows("a", "b", "c")
    )
  })

  it("drops a torrent deleted from a single page instead of resurrecting it", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c"), rows("a", "c"), 2)
    expect(hashes(merged)).toEqual(["a", "c"])
    expect(hashes(merged)).not.toContain("b")
  })

  it("preserves pagination-loaded later pages while page 0 stays authoritative", () => {
    // page size 2: prev = page0[a,b] + page1[c,d]; the fresh page 0 is still [a,b].
    expect(mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("a", "b"), 4)).toEqual(
      rows("a", "b", "c", "d")
    )
  })

  it("does not resurrect a deleted page-0 torrent when later pages are loaded", () => {
    // Regression for the adversarial-review finding. Page size 2, 4 rows loaded
    // (page0[a,b] + page1[c,d]). 'a' is deleted, so the fresh page 0 becomes [b,c]
    // (c shifts up) and total drops to 3. The previous merge re-added 'a' from the
    // replaced window and then sliced off a real trailing row.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("b", "c"), 3)
    expect(hashes(merged)).not.toContain("a")
    expect(hashes(merged)).toEqual(["b", "c", "d"])
  })

  it("caps the merged result to total", () => {
    expect(mergeStreamedFirstPage(rows("a", "b"), rows("a"), 1)).toEqual(rows("a"))
  })
})
