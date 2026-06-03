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
    // Single page (no pagination): prev fits in one page-0 window, so the fresh page is
    // authoritative for every row. 'b' is gone from the fresh page and must not survive.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c"), rows("a", "c"), 2)
    expect(hashes(merged)).toEqual(["a", "c"])
    expect(hashes(merged)).not.toContain("b")
  })

  it("drops the last-sorted torrent when it is deleted from a single page", () => {
    // Single complete page (total === fresh page length): the LAST row 'c' is deleted.
    // The fresh page is the whole truth, so 'c' must not be resurrected from the prior
    // list. This is the tail-deletion case the cap used to handle and that a naive
    // dedup-only merge would regress.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c"), rows("a", "b"), 2)
    expect(hashes(merged)).toEqual(["a", "b"])
    expect(hashes(merged)).not.toContain("c")
  })

  it("preserves pagination-loaded later pages while page 0 stays authoritative", () => {
    // page size 2: prev = page0[a,b] + page1[c,d]; the fresh page 0 is still [a,b].
    expect(mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("a", "b"), 4)).toEqual(
      rows("a", "b", "c", "d")
    )
  })

  it("does not resurrect a page-0 torrent that reflowed off the window when later pages are loaded", () => {
    // Page size 2, 4 rows loaded (page0[a,b] + page1[c,d]). 'a' is deleted, so the fresh
    // page 0 becomes [b,c] (c reflows up) and total drops to 3. The merge must not
    // re-add 'a' from the replaced window, and must keep the surviving trailing row 'd'.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("b", "c"), 3)
    expect(hashes(merged)).not.toContain("a")
    expect(hashes(merged)).toEqual(["b", "c", "d"])
  })

  it("keeps the deleted later-page culprit unknowable instead of dropping a survivor (repro)", () => {
    // Reproduction from the adversarial-review finding. page size 3, 6 rows loaded
    // (page0[a,b,c] + page1[d,e,f]). A later-page torrent (say 'd') is deleted, so the
    // page-0 payload is unchanged [a,b,c] but total drops to 5. Page-0 data cannot reveal
    // which trailing row was removed.
    //
    // The old implementation tail-capped to total, returning [a,b,c,d,e] -- it kept the
    // deleted 'd' AND dropped the surviving 'f'. The set-correct behavior keeps every
    // trailing row the client still holds (never hiding a survivor); at worst one stale
    // later-page row lingers until the page is refetched.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e", "f"), rows("a", "b", "c"), 5)
    expect(hashes(merged)).toEqual(["a", "b", "c", "d", "e", "f"])
    // Crucially, the genuine survivor 'f' is never dropped to satisfy the cap.
    expect(hashes(merged)).toContain("f")
  })

  it("keeps all later-page rows when a later-page row is deleted (page 0 unchanged)", () => {
    // page size 2: prev = page0[a,b] + page1[c,d]. 'd' deleted server-side, total -> 3,
    // page 0 stays [a,b]. The surviving later-page row 'c' must remain; we cannot tell
    // that 'd' specifically went, so it lingers rather than risking dropping 'c'.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("a", "b"), 3)
    expect(hashes(merged)).toEqual(["a", "b", "c", "d"])
    expect(hashes(merged)).toContain("c")
  })

  it("drops multiple page-0 window deletions when later pages refill the window (multiple deletes)", () => {
    // page size 3: prev = page0[a,b,c] + page1[d,e,f] + page2[g]. 'a' and 'b' are deleted,
    // so the full fresh page 0 reflows to [c,d,e] (still 3 rows) and total drops to 5. Both
    // deleted hashes must be gone, the reflowed 'd'/'e' must not duplicate, and surviving
    // trailing rows 'f' and 'g' must remain.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e", "f", "g"), rows("c", "d", "e"), 5)
    expect(hashes(merged)).toEqual(["c", "d", "e", "f", "g"])
    expect(hashes(merged)).not.toContain("a")
    expect(hashes(merged)).not.toContain("b")
  })

  it("drops the page-0 window deletion but keeps later pages on a multi-page delete", () => {
    // page size 3: prev = page0[a,b,c] + page1[d,e]. 'b' deleted on page 0, so the fresh
    // page reflows to [a,c,d] and total -> 4. 'b' must be gone, 'e' (survivor) must stay,
    // and 'd' must not be duplicated even though it appears in both the fresh page and the
    // old trailing slice.
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e"), rows("a", "c", "d"), 4)
    expect(hashes(merged)).toEqual(["a", "c", "d", "e"])
    expect(hashes(merged)).not.toContain("b")
  })

  it("treats a fresh page that covers the whole result as authoritative", () => {
    // page size 1: prev = page0[a] + page1[b]. total drops to 1, so only one torrent
    // exists now and the fresh page 0 ([a]) already covers it. 'b' is therefore the row
    // that was deleted and must not linger. (When total > the fresh page length we keep
    // trailing rows instead - see the paginated cases above.)
    const merged = mergeStreamedFirstPage(rows("a", "b"), rows("a"), 1)
    expect(hashes(merged)).toEqual(["a"])
    expect(hashes(merged)).not.toContain("b")
  })

  describe("with a custom key extractor (cross-instance identity)", () => {
    // Cross-instance rows are identified by instanceId+hash, because the same torrent
    // cross-seeded onto two instances shares a hash but is two distinct rows.
    type Row = { hash: string; instanceId: number }
    const keyOf = (row: Row) => `${row.instanceId}:${row.hash}`
    const row = (hash: string, instanceId: number): Row => ({ hash, instanceId })
    const keys = (list: Row[]) => list.map(keyOf)

    it("keeps cross-seeded same-hash rows on different instances as distinct trailing rows", () => {
      // page size 2: prev = page0[x@1, y@1] + page1[x@2, z@1]; total 4. The fresh page 0
      // is still [x@1, y@1]. A hash-only key would treat trailing 'x@2' as a duplicate of
      // page-0 'x@1' and drop it; the instanceId+hash key must preserve it.
      const prev = [row("x", 1), row("y", 1), row("x", 2), row("z", 1)]
      const next = [row("x", 1), row("y", 1)]
      const merged = mergeStreamedFirstPage(prev, next, 4, keyOf)
      expect(keys(merged)).toEqual(["1:x", "1:y", "2:x", "1:z"])
    })

    it("still de-dupes a trailing row that genuinely reflowed up into page 0", () => {
      // 'x@2' reflowed into the fresh page 0 and also lingers in the old trailing slice;
      // it must appear once, identified by instanceId+hash.
      const prev = [row("x", 1), row("y", 1), row("x", 2), row("z", 1)]
      const next = [row("x", 1), row("x", 2)]
      const merged = mergeStreamedFirstPage(prev, next, 4, keyOf)
      expect(keys(merged)).toEqual(["1:x", "2:x", "1:z"])
    })
  })
})
