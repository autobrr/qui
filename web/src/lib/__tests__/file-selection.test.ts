/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { collectFileIndicesInRange } from "@/lib/file-selection"

// Helpers mirroring the flat-row shape: file rows carry a node.file.index,
// folder rows have no file.
const fileRow = (index: number) => ({ node: { file: { index } } })
const folderRow = () => ({ node: {} })

describe("collectFileIndicesInRange", () => {
  it("collects file indices across a forward range (inclusive)", () => {
    const rows = [fileRow(10), fileRow(11), fileRow(12), fileRow(13)]
    expect(collectFileIndicesInRange(rows, 1, 3)).toEqual([11, 12, 13])
  })

  it("handles a reversed range (current before anchor)", () => {
    const rows = [fileRow(10), fileRow(11), fileRow(12), fileRow(13)]
    expect(collectFileIndicesInRange(rows, 3, 1)).toEqual([11, 12, 13])
  })

  it("returns a single index when anchor == current", () => {
    const rows = [fileRow(10), fileRow(11), fileRow(12)]
    expect(collectFileIndicesInRange(rows, 1, 1)).toEqual([11])
  })

  it("excludes folder rows but includes file rows between them", () => {
    const rows = [fileRow(0), folderRow(), fileRow(1), fileRow(2), folderRow()]
    expect(collectFileIndicesInRange(rows, 0, 4)).toEqual([0, 1, 2])
  })

  it("ignores out-of-range positions", () => {
    const rows = [fileRow(5), fileRow(6)]
    expect(collectFileIndicesInRange(rows, 0, 99)).toEqual([5, 6])
  })

  it("returns empty when both positions are beyond the array", () => {
    const rows = [fileRow(5), fileRow(6)]
    expect(collectFileIndicesInRange(rows, 10, 99)).toEqual([])
  })

  it("returns empty for an empty rows input", () => {
    expect(collectFileIndicesInRange([], 0, 0)).toEqual([])
  })

  it("returns an empty array when the range contains no files", () => {
    const rows = [folderRow(), folderRow()]
    expect(collectFileIndicesInRange(rows, 0, 1)).toEqual([])
  })
})
