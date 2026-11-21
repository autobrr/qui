/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { resolve } from "node:path"

import { describe, expect, it } from "vitest"

import { autoLettersFor, INSTANCE_MARK_HUES, normalizeInstanceMarkLetters, parseInstanceMark } from "@/lib/instance-marks"

describe("normalizeInstanceMarkLetters", () => {
  it("keeps the first two characters in upper case", () => {
    expect(normalizeInstanceMarkLetters(" abc ")).toBe("AB")
  })

  it("counts an emoji as one character", () => {
    expect(normalizeInstanceMarkLetters("👨‍👩‍👧x")).toBe("👨‍👩‍👧X")
  })

  it("drops the spaces between characters", () => {
    expect(normalizeInstanceMarkLetters("a b")).toBe("AB")
  })

  it("gives the same result when it runs twice", () => {
    for (const value of ["ab", "ßx", "é1", "👍🏽z", "  q", "a b", ""]) {
      const once = normalizeInstanceMarkLetters(value)
      expect(normalizeInstanceMarkLetters(once)).toBe(once)
    }
  })
})

describe("parseInstanceMark", () => {
  it("reads a stored mark", () => {
    expect(parseInstanceMark(JSON.stringify({ icon: "cat", color: "teal", letters: "" }))).toEqual({ icon: "cat", color: "teal", letters: "" })
  })

  it.each(["rose", "constructor"])("drops a color that the palette does not have: %s", (color) => {
    expect(parseInstanceMark(JSON.stringify({ icon: null, color, letters: "nas" })).color).toBeNull()
  })

  it("normalizes stored letters and drops an empty icon", () => {
    expect(parseInstanceMark(JSON.stringify({ icon: "", letters: "nas" }))).toEqual({ icon: null, color: null, letters: "NA" })
  })

  it("throws on a value that is not an object", () => {
    expect(() => parseInstanceMark("\"cat\"")).toThrow()
  })
})

describe("autoLettersFor", () => {
  const instances = [
    { id: 1, name: "Seedbox", isActive: true },
    { id: 2, name: "Sr", isActive: false },
    { id: 3, name: "Server", isActive: true },
  ]

  it("gives an active instance the initials that the sidebar shows, where disabled instances are missing", () => {
    expect(autoLettersFor(instances, 3)).toBe("SR")
  })

  it("gives a disabled instance the initials it would have in its place in the list", () => {
    expect(autoLettersFor(instances, 2)).toBe("SR")
  })
})

describe("INSTANCE_MARK_HUES", () => {
  const indexCss = readFileSync(resolve(__dirname, "../../index.css"), "utf8")
  const defaultThemeDestructiveHues = Array.from(indexCss.matchAll(/--destructive:\s*oklch\([\d.]+ [\d.]+ ([\d.]+)\)/gu), (match) => Number(match[1]))
  const hueDistance = (a: number, b: number) => Math.min(Math.abs(a - b), 360 - Math.abs(a - b))

  it("keeps each color at least 20 degrees from the red of the default theme's destructive color in index.css", () => {
    expect(defaultThemeDestructiveHues).toHaveLength(2)
    for (const hue of Object.values(INSTANCE_MARK_HUES)) {
      for (const problemHue of defaultThemeDestructiveHues) {
        expect(hueDistance(hue, problemHue)).toBeGreaterThanOrEqual(20)
      }
    }
  })
})
