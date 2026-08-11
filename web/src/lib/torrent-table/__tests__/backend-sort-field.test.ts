/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getBackendSortField } from "@/lib/torrent-table/backend-sort-field"
import { describe, expect, it } from "vitest"

describe("getBackendSortField", () => {
  it("remaps the columns whose backend field differs from the column id", () => {
    expect(getBackendSortField("status_icon")).toBe("state")
    expect(getBackendSortField("num_seeds")).toBe("num_complete")
    expect(getBackendSortField("num_leechs")).toBe("num_incomplete")
  })

  it("defaults to added_on for an empty column id", () => {
    expect(getBackendSortField("")).toBe("added_on")
  })

  it("passes unmapped column ids through unchanged", () => {
    expect(getBackendSortField("size")).toBe("size")
    expect(getBackendSortField("name")).toBe("name")
  })
})
