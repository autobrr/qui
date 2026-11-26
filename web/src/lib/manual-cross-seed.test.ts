/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { canOfferManualCrossSeed, overlapPercent } from "./manual-cross-seed"

describe("canOfferManualCrossSeed", () => {
  it("allows exactly one file with no urls", () => {
    expect(canOfferManualCrossSeed({ fileCount: 1, urlText: "" })).toBe(true)
  })

  it("rejects multi-file adds and mixed input", () => {
    expect(canOfferManualCrossSeed({ fileCount: 0, urlText: "" })).toBe(false)
    expect(canOfferManualCrossSeed({ fileCount: 2, urlText: "" })).toBe(false)
    expect(canOfferManualCrossSeed({ fileCount: 1, urlText: "magnet:?xt=urn:btih:abc" })).toBe(false)
  })
})

describe("overlapPercent", () => {
  it("clamps and rounds", () => {
    expect(overlapPercent(1)).toBe(100)
    expect(overlapPercent(0.499)).toBe(50)
    expect(overlapPercent(0)).toBe(0)
    expect(overlapPercent(-1)).toBe(0)
    expect(overlapPercent(2)).toBe(100)
  })
})
