/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { reconcileExpandedFolders } from "./file-tree-expansion"

describe("reconcileExpandedFolders", () => {
  it("keeps a renamed folder expanded (old path dropped, new path auto-expanded)", () => {
    const expanded = new Set(["Old", "Old/sub"])
    const previousIds = new Set(["Old", "Old/sub"])
    const currentIds = new Set(["New", "New/sub"])

    const next = reconcileExpandedFolders(expanded, previousIds, currentIds)

    expect(next).toEqual(new Set(["New", "New/sub"]))
  })

  it("does not re-expand a user-collapsed folder when ids are unchanged", () => {
    const expanded = new Set(["a"])
    const ids = new Set(["a", "b"])

    const next = reconcileExpandedFolders(expanded, ids, ids)

    expect(next).toBe(expanded)
  })

  it("preserves collapsed state for surviving folders while expanding new ones", () => {
    const expanded = new Set(["a"])
    const previousIds = new Set(["a", "b"])
    const currentIds = new Set(["a", "b", "c"])

    const next = reconcileExpandedFolders(expanded, previousIds, currentIds)

    expect(next).toEqual(new Set(["a", "c"]))
  })

  it("expands everything when all ids are new (torrent switch)", () => {
    const expanded = new Set(["old"])
    const previousIds = new Set(["old"])
    const currentIds = new Set(["x", "x/y"])

    const next = reconcileExpandedFolders(expanded, previousIds, currentIds)

    expect(next).toEqual(new Set(["x", "x/y"]))
  })
})
