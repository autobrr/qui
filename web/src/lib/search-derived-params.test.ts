/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { resolveSuggestionIndexerIds } from "./search-derived-params"

describe("resolveSuggestionIndexerIds", () => {
  it("keeps only the saved ids that are still enabled", () => {
    expect(resolveSuggestionIndexerIds([3, 1, 7], [1, 2, 3])).toEqual(new Set([3, 1]))
  })

  it("returns null when no saved ids are given", () => {
    expect(resolveSuggestionIndexerIds([], [1, 2])).toBeNull()
    expect(resolveSuggestionIndexerIds(null, [1, 2])).toBeNull()
    expect(resolveSuggestionIndexerIds(undefined, [1, 2])).toBeNull()
  })

  it("returns null when saved and enabled ids do not overlap", () => {
    expect(resolveSuggestionIndexerIds([4, 5], [1, 2, 3])).toBeNull()
    expect(resolveSuggestionIndexerIds([1, 2], [])).toBeNull()
  })
})
