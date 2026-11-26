/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn, formatBytesOrFallback, joinPath, parseTrackerDomains, takeTurns } from "@/lib/utils"
import type { Automation } from "@/types"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { describe, expect, it } from "vitest"
import "@/i18n"

function makeAutomation(overrides: Partial<Automation> = {}): Automation {
  return {
    id: 1,
    instanceId: 1,
    name: "My workflow",
    trackerPattern: "",
    conditions: { schemaVersion: "1" },
    enabled: true,
    dryRun: false,
    notify: true,
    sortOrder: 0,
    ...overrides,
  }
}

describe("parseTrackerDomains", () => {
  it("strips negation from trackerPattern tokens", () => {
    const result = parseTrackerDomains(makeAutomation({
      trackerPattern: "a.com,!b.com;c.com",
    }))

    expect(result).toEqual(["a.com", "b.com", "c.com"])
  })
})

describe("cn with a named font size", () => {
  const indexCss = readFileSync(resolve(__dirname, "../../index.css"), "utf8")
  const namedSizes = [...indexCss.matchAll(/--text-(\w+):/g)].map(([, name]) => `text-${name}`)

  it.each(namedSizes)("keeps a text color and drops an earlier font size and line height: %s", (size) => {
    expect(cn(`text-red-500 text-xs leading-none ${size}`)).toBe(`text-red-500 ${size}`)
  })

  it.each([
    ["a variant", "md:text-red-500 md:text-sm md:leading-none md:text-13", "md:text-red-500 md:text-13"],
    ["a line-height modifier", "text-red-500 text-sm leading-none text-13/5", "text-red-500 text-13/5"],
  ])("does the same with %s", (_, classes, merged) => {
    expect(cn(classes)).toBe(merged)
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

describe("takeTurns", () => {
  it("takes one item from each list in turn, so a long list cannot fill a sample", () => {
    expect(takeTurns([["a1", "a2", "a3"], ["b1"], [], ["c1", "c2"]]).slice(0, 4)).toEqual(["a1", "b1", "c1", "a2"])
    expect(takeTurns([])).toEqual([])
  })
})
