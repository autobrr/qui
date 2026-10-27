/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { combineFilterExpr, isCrossSeedExpr } from "./torrent-filters"

describe("combineFilterExpr", () => {
  it.each([
    ["both sides", "size > 1", "state == \"downloading\"", "(size > 1) && (state == \"downloading\")"],
    ["column only", "size > 1", undefined, "size > 1"],
    ["expr only", null, "state == \"downloading\"", "state == \"downloading\""],
    ["empty expr counts as absent", "size > 1", "", "size > 1"],
    ["neither", null, undefined, undefined],
    ["neither, empty strings", "", "", undefined],
  ])("%s", (_, columnExpr, expr, want) => {
    expect(combineFilterExpr(columnExpr, expr)).toBe(want)
  })
})

describe("isCrossSeedExpr", () => {
  it.each([
    ["two hashes", "Hash == \"a\" || Hash == \"b\"", true],
    ["one hash, no OR", "Hash == \"a\"", false],
    ["OR without hashes", "size > 1 || size < 0", false],
    ["undefined", undefined, false],
  ])("%s", (_, expr, want) => {
    expect(isCrossSeedExpr(expr)).toBe(want)
  })
})
