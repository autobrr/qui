/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { nextSelection } from "./multi-select"

describe("nextSelection", () => {
  it("appends when multiple values are allowed", () => {
    expect(nextSelection(["a"], "b", false)).toEqual(["a", "b"])
  })

  it("replaces instead of appending in single mode", () => {
    // Regression: appending put the new value at index 1, and callers that store
    // one string read index 0, so picking a second option looked like a no-op.
    expect(nextSelection(["a"], "b", true)).toEqual(["b"])
  })

  it("toggles a selected value off in either mode", () => {
    expect(nextSelection(["a", "b"], "a", false)).toEqual(["b"])
    expect(nextSelection(["a"], "a", true)).toEqual([])
  })

  it("selects into an empty list", () => {
    expect(nextSelection([], "a", true)).toEqual(["a"])
    expect(nextSelection([], "a", false)).toEqual(["a"])
  })
})
