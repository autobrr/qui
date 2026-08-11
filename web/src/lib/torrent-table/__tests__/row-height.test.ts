/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { viewModeRowHeight } from "@/lib/torrent-table/row-height"
import { describe, expect, it } from "vitest"

describe("viewModeRowHeight", () => {
  it("maps each view mode to its row height", () => {
    expect(viewModeRowHeight("compact")).toBe(80)
    expect(viewModeRowHeight("dense")).toBe(26)
    expect(viewModeRowHeight("normal")).toBe(40)
  })

  it("falls back to the normal height for any other mode (default branch)", () => {
    expect(viewModeRowHeight("ultra-compact")).toBe(40)
  })
})
