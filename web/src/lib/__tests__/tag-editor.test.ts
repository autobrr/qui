/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  buildTagEditorItems,
  buildTagUpdatePlan,
  cycleTagSelectionState,
  hasTagUpdatePlan,
  sortTags,
  type TagEditorItem
} from "@/lib/tag-editor"
import { describe, expect, it } from "vitest"

describe("sortTags", () => {
  it("sorts numerically aware (tag10 after tag2)", () => {
    expect(sortTags(["tag10", "tag2", "tag1"])).toEqual(["tag1", "tag2", "tag10"])
  })

  it("accepts any iterable", () => {
    expect(sortTags(new Set(["b", "a"]))).toEqual(["a", "b"])
  })
})

describe("buildTagEditorItems", () => {
  it("returns empty when no tags are available", () => {
    expect(buildTagEditorItems([], [], 0)).toEqual([])
    expect(buildTagEditorItems(null, [], 0)).toEqual([])
  })

  it("marks a tag 'on' only when EVERY selected torrent has it", () => {
    const items = buildTagEditorItems(
      [],
      ["linux,iso", "linux,iso", "linux,iso"],
      3
    )
    const linux = items.find(i => i.tag === "linux")
    expect(linux?.initialState).toBe("on")
    expect(linux?.state).toBe("on")
  })

  it("marks a tag 'mixed' when some but not all selected torrents have it", () => {
    const items = buildTagEditorItems(
      [],
      ["linux,iso", "linux", "windows"],
      3
    )
    expect(items.find(i => i.tag === "linux")?.initialState).toBe("mixed")
    expect(items.find(i => i.tag === "iso")?.initialState).toBe("mixed")
    expect(items.find(i => i.tag === "windows")?.initialState).toBe("mixed")
  })

  it("marks an available-but-unused tag 'off'", () => {
    const items = buildTagEditorItems(
      ["unused"],
      ["linux"],
      1
    )
    expect(items.find(i => i.tag === "unused")?.initialState).toBe("off")
  })

  it("counts each torrent at most once per tag (duplicate tags within one torrent don't inflate the count)", () => {
    const items = buildTagEditorItems(
      [],
      ["linux,linux", "windows"],
      2
    )
    expect(items.find(i => i.tag === "linux")?.initialState).toBe("mixed")
  })

  it("sorts items numerically (tag1, tag2, tag10)", () => {
    const items = buildTagEditorItems(["tag10", "tag2", "tag1"], [], 0)
    expect(items.map(i => i.tag)).toEqual(["tag1", "tag2", "tag10"])
  })

  it("does not mark tags 'on' when selectedCount is zero", () => {
    const items = buildTagEditorItems(["x"], [], 0)
    expect(items.find(i => i.tag === "x")?.initialState).toBe("off")
  })

  it("includes available tags even if no torrent uses them, and torrent tags even if not in available", () => {
    const items = buildTagEditorItems(["only-available"], ["only-on-torrents"], 1)
    expect(items.map(i => i.tag).sort()).toEqual(["only-available", "only-on-torrents"])
  })
})

describe("cycleTagSelectionState", () => {
  it.each<["on" | "off" | "mixed", "on" | "off"]>([
    ["mixed", "on"],
    ["on", "off"],
    ["off", "on"],
  ])("%s -> %s", (current, next) => {
    expect(cycleTagSelectionState(current)).toBe(next)
  })
})

describe("buildTagUpdatePlan", () => {
  const item = (overrides: Partial<TagEditorItem>): TagEditorItem => ({
    tag: "x",
    initialState: "off",
    state: "off",
    ...overrides,
  })

  it("adds tags that flipped to 'on' from off or mixed", () => {
    expect(buildTagUpdatePlan([
      item({ tag: "newly-on", initialState: "off", state: "on" }),
      item({ tag: "flipped-from-mixed", initialState: "mixed", state: "on" }),
    ])).toEqual({ add: ["newly-on", "flipped-from-mixed"], remove: [] })
  })

  it("removes tags that flipped to 'off' from on or mixed", () => {
    expect(buildTagUpdatePlan([
      item({ tag: "newly-off", initialState: "on", state: "off" }),
      item({ tag: "flipped-from-mixed", initialState: "mixed", state: "off" }),
    ])).toEqual({ add: [], remove: ["newly-off", "flipped-from-mixed"] })
  })

  it("does not emit operations for unchanged items", () => {
    expect(buildTagUpdatePlan([
      item({ tag: "still-on", initialState: "on", state: "on" }),
      item({ tag: "still-off", initialState: "off", state: "off" }),
      item({ tag: "still-mixed", initialState: "mixed", state: "mixed" }),
    ])).toEqual({ add: [], remove: [] })
  })

  it("preserves the input order in both add and remove lists", () => {
    const plan = buildTagUpdatePlan([
      item({ tag: "a", initialState: "off", state: "on" }),
      item({ tag: "b", initialState: "on", state: "off" }),
      item({ tag: "c", initialState: "off", state: "on" }),
      item({ tag: "d", initialState: "on", state: "off" }),
    ])
    expect(plan.add).toEqual(["a", "c"])
    expect(plan.remove).toEqual(["b", "d"])
  })
})

describe("hasTagUpdatePlan", () => {
  it.each<[{ add: string[]; remove: string[] }, boolean]>([
    [{ add: [], remove: [] }, false],
    [{ add: ["a"], remove: [] }, true],
    [{ add: [], remove: ["b"] }, true],
  ])("hasTagUpdatePlan(%j) -> %s", (plan, expected) => {
    expect(hasTagUpdatePlan(plan)).toBe(expected)
  })
})
