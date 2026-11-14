/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { buildEventDays } from "@/lib/statistics-events"
import type { TransferHistoryLibraryRow } from "@/types"

function day(iso: string, added: number, completed: number, deleted: number): TransferHistoryLibraryRow {
  return { day: iso, added, completed, deleted }
}

describe("buildEventDays", () => {
  it("adds the instances together on each UTC day of the period and fills the days without a row", () => {
    const days = buildEventDays(
      [[day("2026-03-01T00:00:00Z", 2, 1, 0), day("2026-03-03T00:00:00Z", 0, 0, 4)], [day("2026-03-01T00:00:00Z", 1, 0, 1)]],
      { from: new Date("2026-03-01T00:00:00Z"), to: new Date("2026-03-04T00:00:00Z") },
      undefined
    )
    expect(days?.map(({ added, completed, deleted }) => [added, completed, deleted])).toEqual([[3, 1, 1], [0, 0, 0], [0, 0, 4]])
    expect(days?.map(({ start }) => new Date(start).getDate())).toEqual([1, 2, 3])
  })

  it("starts a period with no start at the first recording", () => {
    const days = buildEventDays([[]], { to: new Date("2026-03-04T00:00:00Z") }, new Date("2026-03-02T10:00:00Z"))
    expect(days).toHaveLength(2)
    expect(buildEventDays([[]], { to: new Date("2026-03-04T00:00:00Z") }, undefined)).toBeUndefined()
  })
})
