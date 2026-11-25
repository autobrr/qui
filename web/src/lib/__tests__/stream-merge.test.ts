/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { applyOrderedDelta, mergeStreamedFirstPage } from "@/lib/stream-merge"

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

  it("drops the last-sorted torrent when it is deleted from a single page", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c"), rows("a", "b"), 2)
    expect(hashes(merged)).toEqual(["a", "b"])
    expect(hashes(merged)).not.toContain("c")
  })

  it("preserves pagination-loaded later pages while page 0 stays authoritative", () => {
    expect(mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("a", "b"), 4)).toEqual(
      rows("a", "b", "c", "d")
    )
  })

  it("does not resurrect a page-0 torrent that reflowed off the window when later pages are loaded", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d"), rows("b", "c"), 3)
    expect(hashes(merged)).not.toContain("a")
    expect(hashes(merged)).toEqual(["b", "c", "d"])
  })

  it("keeps the deleted later-page culprit unknowable instead of dropping a survivor (repro)", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e", "f"), rows("a", "b", "c"), 5)
    expect(hashes(merged)).toEqual(["a", "b", "c", "d", "e", "f"])
    expect(hashes(merged)).toContain("f")
  })

  it("drops multiple page-0 window deletions when later pages refill the window (multiple deletes)", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e", "f", "g"), rows("c", "d", "e"), 5)
    expect(hashes(merged)).toEqual(["c", "d", "e", "f", "g"])
    expect(hashes(merged)).not.toContain("a")
    expect(hashes(merged)).not.toContain("b")
  })

  it("drops the page-0 window deletion but keeps later pages on a multi-page delete", () => {
    const merged = mergeStreamedFirstPage(rows("a", "b", "c", "d", "e"), rows("a", "c", "d"), 4)
    expect(hashes(merged)).toEqual(["a", "c", "d", "e"])
    expect(hashes(merged)).not.toContain("b")
  })

  describe("with a custom key extractor (cross-instance identity)", () => {
    type Row = { hash: string; instanceId: number }
    const keyOf = (row: Row) => `${row.instanceId}:${row.hash}`
    const row = (hash: string, instanceId: number): Row => ({ hash, instanceId })
    const keys = (list: Row[]) => list.map(keyOf)

    it("keeps cross-seeded same-hash rows on different instances as distinct trailing rows", () => {
      const prev = [row("x", 1), row("y", 1), row("x", 2), row("z", 1)]
      const next = [row("x", 1), row("y", 1)]
      const merged = mergeStreamedFirstPage(prev, next, 4, keyOf)
      expect(keys(merged)).toEqual(["1:x", "1:y", "2:x", "1:z"])
    })

    it("still de-dupes a trailing row that genuinely reflowed up into page 0", () => {
      const prev = [row("x", 1), row("y", 1), row("x", 2), row("z", 1)]
      const next = [row("x", 1), row("x", 2)]
      const merged = mergeStreamedFirstPage(prev, next, 4, keyOf)
      expect(keys(merged)).toEqual(["1:x", "2:x", "1:z"])
    })
  })
})

describe("mergeStreamedFirstPage with Copy groups", () => {
  type GroupedRow = Row & { copy_group?: { id: string; first_sorted?: { instance_id: number; hash: string } } }
  const top = (hash: string, id?: string): GroupedRow =>
    id ? { hash, copy_group: { id, first_sorted: { instance_id: 1, hash } } } : { hash }
  const member = (hash: string, id: string): GroupedRow => ({ hash, copy_group: { id } })

  it("keeps the later pages when page 0 gets longer because a group gained a copy", () => {
    const prev = [top("A", "g"), member("a1", "g"), top("B"), top("C"), top("D")]
    const next = [top("A", "g"), member("a1", "g"), member("a2", "g"), top("B")]
    expect(hashes(mergeStreamedFirstPage(prev, next, 6))).toEqual(["A", "a1", "a2", "B", "C", "D"])
  })

  it("drops the old members of page 0 when page 0 gets shorter", () => {
    const prev = [top("A", "g"), member("a1", "g"), member("a2", "g"), top("B"), top("C")]
    const next = [top("A"), top("B")]
    expect(hashes(mergeStreamedFirstPage(prev, next, 3))).toEqual(["A", "B", "C"])
  })

  it("keeps the later members of a group whose representative moved up into page 0", () => {
    const prev = [top("A"), member("a1", "g"), member("a2", "g"), top("B")]
    const next = [top("A")]
    expect(hashes(mergeStreamedFirstPage(prev, next, 4))).toEqual(["A", "a1", "a2", "B"])
  })
})

describe("applyOrderedDelta", () => {
  type Named = { hash: string; name: string }
  const named = (hash: string, name = hash): Named => ({ hash, name })
  const keyOf = (row: Named) => row.hash
  const order = (list: Named[]) => list.map(row => row.hash)

  it("returns the previous rows by reference on an aggregate-only tick", () => {
    const prev = [named("a"), named("b")]
    const result = applyOrderedDelta(prev, [], undefined, keyOf)
    expect(result.changed).toBe(false)
    expect(result.rows).toBe(prev)
  })

  it("applies an in-place value change while keeping unchanged rows referentially stable", () => {
    const a = named("a")
    const b = named("b")
    const bUpdated = named("b", "b-new")
    const { rows, changed } = applyOrderedDelta([a, b], [bUpdated], undefined, keyOf)
    expect(changed).toBe(true)
    expect(order(rows)).toEqual(["a", "b"])
    expect(rows[0]).toBe(a)
    expect(rows[1]).toBe(bUpdated)
  })

  it("inserts an added row at the position given by order", () => {
    const { rows } = applyOrderedDelta(
      [named("a"), named("b")],
      [named("d")],
      ["a", "d", "b"],
      keyOf
    )
    expect(order(rows)).toEqual(["a", "d", "b"])
  })

  it("drops a removed row via the new order", () => {
    const { rows } = applyOrderedDelta(
      [named("a"), named("b"), named("c")],
      [],
      ["a", "c"],
      keyOf
    )
    expect(order(rows)).toEqual(["a", "c"])
  })

  it("clears the page on a present-but-empty order (N->0 delete), not treating it as aggregate-only", () => {
    const result = applyOrderedDelta([named("a"), named("b")], [], [], keyOf)
    expect(result.changed).toBe(true)
    expect(result.rows).toEqual([])
  })

  it("reorders existing rows without new payloads, preserving references", () => {
    const a = named("a")
    const b = named("b")
    const c = named("c")
    const { rows } = applyOrderedDelta([a, b, c], [], ["c", "b", "a"], keyOf)
    expect(order(rows)).toEqual(["c", "b", "a"])
    expect(rows[0]).toBe(c)
    expect(rows[2]).toBe(a)
  })

  it("ignores an order key with no known row (defensive against a missed add)", () => {
    const { rows } = applyOrderedDelta([named("a")], [], ["a", "ghost"], keyOf)
    expect(order(rows)).toEqual(["a"])
  })
})
