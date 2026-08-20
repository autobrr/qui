/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { shallowEqualTrackerIcons } from "@/lib/torrent-table/tracker-icon-equality"
import { describe, expect, it } from "vitest"

describe("shallowEqualTrackerIcons", () => {
  it("returns true for the same reference", () => {
    const map = { "a.com": "iconA" }
    expect(shallowEqualTrackerIcons(map, map)).toBe(true)
  })

  it("treats two undefined inputs as equal (same reference)", () => {
    expect(shallowEqualTrackerIcons(undefined, undefined)).toBe(true)
  })

  it("returns false when exactly one side is undefined", () => {
    expect(shallowEqualTrackerIcons({ "a.com": "iconA" }, undefined)).toBe(false)
    expect(shallowEqualTrackerIcons(undefined, { "a.com": "iconA" })).toBe(false)
  })

  it("returns true for distinct objects with identical contents", () => {
    const prev = { "a.com": "iconA", "b.com": "iconB" }
    const next = { "a.com": "iconA", "b.com": "iconB" }
    expect(prev).not.toBe(next)
    expect(shallowEqualTrackerIcons(prev, next)).toBe(true)
  })

  it("returns false when a value differs", () => {
    expect(
      shallowEqualTrackerIcons({ "a.com": "iconA" }, { "a.com": "iconA-v2" })
    ).toBe(false)
  })

  it("returns false when key counts differ", () => {
    expect(
      shallowEqualTrackerIcons({ "a.com": "iconA" }, { "a.com": "iconA", "b.com": "iconB" })
    ).toBe(false)
  })

  it("returns false when keys differ but counts match", () => {
    expect(
      shallowEqualTrackerIcons({ "a.com": "iconA" }, { "b.com": "iconA" })
    ).toBe(false)
  })
})
