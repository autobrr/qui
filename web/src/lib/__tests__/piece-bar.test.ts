/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { computePieceBarColumns, computePieceBarFill } from "@/lib/piece-bar"

const MISSING = 0
const DOWNLOADING = 1
const FINISHED = 2

describe("computePieceBarColumns", () => {
  it("gives a full finished share to a column of finished pieces and none to a column of missing pieces", () => {
    const states = [...Array(6).fill(FINISHED), ...Array(6).fill(MISSING)]

    expect(computePieceBarColumns(states, 2)).toEqual([
      { finished: 1, downloading: 0 },
      { finished: 0, downloading: 0 },
    ])
  })

  // regression: #4093
  it("gives a column with 2 finished pieces of 6 a finished share of one third", () => {
    const states = [MISSING, FINISHED, MISSING, MISSING, FINISHED, MISSING]

    const [column] = computePieceBarColumns(states, 1)

    expect(column.finished).toBeCloseTo(1 / 3, 2)
    expect(column.downloading).toBe(0)
  })

  it("gives a column with 1 downloading piece of 6 a downloading share of one sixth", () => {
    const states = [MISSING, MISSING, DOWNLOADING, MISSING, MISSING, MISSING]

    const [column] = computePieceBarColumns(states, 1)

    expect(column.finished).toBe(0)
    expect(column.downloading).toBeCloseTo(1 / 6, 2)
  })

  it("gives one column for each piece when the bar is wider than the piece count", () => {
    expect(computePieceBarColumns([FINISHED, DOWNLOADING, MISSING], 300)).toEqual([
      { finished: 1, downloading: 0 },
      { finished: 0, downloading: 1 },
      { finished: 0, downloading: 0 },
    ])
  })

  it("counts a piece that falls across two columns in each column by its overlap", () => {
    const [left, right] = computePieceBarColumns([MISSING, FINISHED, MISSING], 2)

    expect(left.finished).toBeCloseTo(1 / 3, 2)
    expect(right.finished).toBeCloseTo(1 / 3, 2)
  })

  // regression: #4093
  it("keeps the mean finished share at the piece count share when finished pieces are spread out", () => {
    const pieceCount = 11592
    const finishedCount = 3955
    const states: number[] = Array(pieceCount).fill(MISSING)
    for (let k = 0; k < finishedCount; k++) {
      states[Math.floor((k * pieceCount) / finishedCount)] = FINISHED
    }

    const columns = computePieceBarColumns(states, 2000)
    const meanFinished = columns.reduce((sum, column) => sum + column.finished, 0) / columns.length

    expect(columns).toHaveLength(2000)
    expect(Math.abs(meanFinished - 0.34)).toBeLessThanOrEqual(0.01)
  })
})

describe("computePieceBarFill", () => {
  it("fills the whole height for a finished column and nothing for a missing column", () => {
    expect(computePieceBarFill({ finished: 1, downloading: 0 }, 12)).toEqual({ finishedPixels: 12, downloadingPixels: 0 })
    expect(computePieceBarFill({ finished: 0, downloading: 0 }, 12)).toEqual({ finishedPixels: 0, downloadingPixels: 0 })
  })

  it("fills a third of the height for a column that is one third finished", () => {
    expect(computePieceBarFill({ finished: 0.33, downloading: 0 }, 12)).toEqual({ finishedPixels: 4, downloadingPixels: 0 })
  })

  it("keeps one pixel for a finished share too small to round to a pixel", () => {
    expect(computePieceBarFill({ finished: 0.01, downloading: 0 }, 12)).toEqual({ finishedPixels: 1, downloadingPixels: 0 })
  })

  it("leaves one pixel empty when a column is almost finished", () => {
    expect(computePieceBarFill({ finished: 0.99, downloading: 0 }, 12)).toEqual({ finishedPixels: 11, downloadingPixels: 0 })
  })

  it("stacks the downloading share on the finished share", () => {
    expect(computePieceBarFill({ finished: 0.5, downloading: 0.17 }, 12)).toEqual({ finishedPixels: 6, downloadingPixels: 2 })
  })

  it("keeps one pixel for a small downloading share on an almost finished column", () => {
    expect(computePieceBarFill({ finished: 0.98, downloading: 0.02 }, 12)).toEqual({ finishedPixels: 11, downloadingPixels: 1 })
  })

  it("stays inside the height when rounded shares add up to more than 1", () => {
    expect(computePieceBarFill({ finished: 0.01, downloading: 1 }, 12)).toEqual({ finishedPixels: 1, downloadingPixels: 11 })
  })
})
