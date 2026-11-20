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

  it("keeps the place of a column that the table does not show, such as instance in the order of every instance", () => {
    const shared = ["name", "instance", "size", "ratio"]

    const result = reorderColumns(shared, "ratio", "size", ["name", "size", "ratio"])

    expect(result).toEqual(["name", "instance", "ratio", "size"])
  })

  it("appends newly-present columns to the end during normalization", () => {
    // active id absent -> returns the normalized order, which now includes "c"
    expect(reorderColumns(["a", "b"], "missing", "b", ["a", "b", "c"])).toEqual(["a", "b", "c"])
  })

  it("returns the order with the appended columns (not the raw input) when a dragged id is missing", () => {
    const currentOrder = ["a", "unseen"]
    const result = reorderColumns(currentOrder, "a", "nope", ["a", "b"])
    expect(result).not.toEqual(currentOrder) // proves normalization still persists
    expect(result).toEqual(["a", "unseen", "b"])
  })

  it("computes indices against the normalized order, not the raw input (off-by-one guard)", () => {
    // raw "c" is at index 2 / "a" at index 0; after "a" is appended they are at 1 / 2
    expect(reorderColumns(["unseen", "c", "b"], "c", "a", ["a", "b", "c"])).toEqual(["unseen", "b", "a", "c"])
  })
})
