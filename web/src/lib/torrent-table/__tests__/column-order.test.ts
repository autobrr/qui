/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { reorderColumns } from "@/lib/torrent-table/column-order"
import { describe, expect, it } from "vitest"

describe("reorderColumns", () => {
  it("moves the active column to the over column's position", () => {
    expect(reorderColumns(["a", "b", "c"], "a", "c", ["a", "b", "c"])).toEqual(["b", "c", "a"])
  })

  it("drops ids that are no longer present in the table's columns", () => {
    const result = reorderColumns(["a", "stale", "b", "c"], "a", "b", ["a", "b", "c"])
    expect(result).not.toContain("stale")
    expect(result).toEqual(["b", "a", "c"])
  })

  it("appends newly-present columns to the end during normalization", () => {
    // active id absent -> returns the normalized order, which now includes "c"
    expect(reorderColumns(["a", "b"], "missing", "b", ["a", "b", "c"])).toEqual(["a", "b", "c"])
  })

  it("returns the sanitized order (not the raw input) when a dragged id is missing", () => {
    const currentOrder = ["a", "stale"]
    const result = reorderColumns(currentOrder, "a", "nope", ["a", "b"])
    expect(result).not.toEqual(currentOrder) // proves normalization still persists
    expect(result).toEqual(["a", "b"])
  })

  it("computes indices against the sanitized order, not the raw input (off-by-one guard)", () => {
    // raw "c" is at index 3 / "a" at index 1; sanitized they are at 2 / 0
    expect(reorderColumns(["stale", "a", "b", "c"], "c", "a", ["a", "b", "c"])).toEqual(["c", "a", "b"])
  })
})
