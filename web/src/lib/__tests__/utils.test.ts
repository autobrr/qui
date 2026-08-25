/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatBytesOrFallback, joinPath, parseTrackerDomains } from "@/lib/utils"
import type { Automation } from "@/types"
import { describe, expect, it } from "vitest"

function makeAutomation(overrides: Partial<Automation> = {}): Automation {
  return {
    id: 1,
    instanceId: 1,
    name: "My workflow",
    trackerPattern: "",
    trackerDomains: [],
    conditions: { schemaVersion: "1" },
    enabled: true,
    dryRun: false,
    notify: true,
    sortOrder: 0,
    ...overrides,
  }
}

describe("parseTrackerDomains", () => {
  it("strips negation from tokenized trackerDomains values", () => {
    const result = parseTrackerDomains(makeAutomation({
      trackerDomains: ["a.com,!b.com;c.com"],
    }))

    expect(result).toEqual(["a.com", "b.com", "c.com"])
  })
})

describe("joinPath", () => {
  it("joins a unix base with a slash-delimited relative path", () => {
    expect(joinPath("/data/torrents", "Movie/file.mkv")).toBe("/data/torrents/Movie/file.mkv")
  })

  it("strips a single trailing separator from the base", () => {
    expect(joinPath("/data/torrents/", "file.mkv")).toBe("/data/torrents/file.mkv")
    expect(joinPath("C:\\dl\\", "file.mkv")).toBe("C:\\dl\\file.mkv")
  })

  it("normalizes relative separators to backslashes for a windows base", () => {
    expect(joinPath("C:\\dl", "Movie/file.mkv")).toBe("C:\\dl\\Movie\\file.mkv")
  })

  it("returns the relative path unchanged when the base is empty", () => {
    expect(joinPath("", "Movie/file.mkv")).toBe("Movie/file.mkv")
  })
})

describe("formatBytesOrFallback", () => {
  it("returns the fallback for a negative byte value", () => {
    expect(formatBytesOrFallback(-1, "Unknown")).toBe("Unknown")
  })

  it.each([
    [0, "0 B"],
    [1024, "1 KiB"],
  ])("formats the available byte value %s", (value, expected) => {
    expect(formatBytesOrFallback(value, "Unknown")).toBe(expected)
  })
})
