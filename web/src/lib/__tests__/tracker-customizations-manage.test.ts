/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { buildTrackerRows, parseTrackerCustomizationsImport, toCustomizationInput } from "@/lib/tracker-customizations-manage"
import type { TrackerCustomization } from "@/types"

function customization(id: number, displayName: string, domains: string[]): TrackerCustomization {
  return { id, displayName, domains, createdAt: "", updatedAt: "" }
}

describe("parseTrackerCustomizationsImport", () => {
  it("accepts an old export that still has includedInStats and drops the field", () => {
    const oldExport = "```json\n" + JSON.stringify({
      comment: "qui tracker customizations for Dashboard",
      trackerCustomizations: [
        { displayName: "Tracker", domains: ["a.example.invalid", "b.example.invalid"], includedInStats: ["b.example.invalid"] },
      ],
    }, null, 2) + "\n```"

    const result = parseTrackerCustomizationsImport(oldExport, [])

    expect(result.valid).toBe(true)
    if (!result.valid) return
    expect(result.entries).toHaveLength(1)
    expect(toCustomizationInput(result.entries[0])).toEqual({
      displayName: "Tracker",
      domains: ["a.example.invalid", "b.example.invalid"],
    })
  })

  it("marks an entry that shares a domain with a customization as a conflict, and an equal one as unchanged", () => {
    const existing = [customization(7, "Tracker", ["a.example.invalid"])]
    const text = JSON.stringify({
      trackerCustomizations: [
        { displayName: "Other", domains: ["A.example.invalid", "c.example.invalid"] },
        { displayName: "Tracker", domains: ["a.example.invalid"] },
        { displayName: "New", domains: ["d.example.invalid"] },
      ],
    })

    const result = parseTrackerCustomizationsImport(text, existing)

    expect(result.valid).toBe(true)
    if (!result.valid) return
    expect(result.entries.map(e => [e.displayName, e.conflict?.id ?? null, e.isIdentical])).toEqual([
      ["Other", 7, false],
      ["Tracker", 7, true],
      ["New", null, false],
    ])
  })

  // regression: #4108
  it("treats the same domains in another order as a conflict, because the first domain is the primary", () => {
    const existing = [customization(7, "Tracker", ["a.example.invalid", "b.example.invalid"])]
    const text = JSON.stringify({ trackerCustomizations: [{ displayName: "Tracker", domains: ["b.example.invalid", "a.example.invalid"] }] })

    const result = parseTrackerCustomizationsImport(text, existing)

    expect(result.valid).toBe(true)
    if (!result.valid) return
    expect(result.entries[0].conflict?.id).toBe(7)
    expect(result.entries[0].isIdentical).toBe(false)
  })

  // regression: #4108
  it("cleans the domains as the server stores them, so a padded domain still conflicts", () => {
    const existing = [customization(7, "Tracker", ["a.example.invalid"])]
    const text = JSON.stringify({ trackerCustomizations: [{ displayName: "Tracker", domains: [" A.example.invalid ", "a.example.invalid", ""] }] })

    const result = parseTrackerCustomizationsImport(text, existing)

    expect(result.valid).toBe(true)
    if (!result.valid) return
    expect(result.entries[0]).toMatchObject({ domains: ["a.example.invalid"], isIdentical: true })
    expect(result.entries[0].conflict?.id).toBe(7)
  })

  it("names the fault of a text it cannot import", () => {
    expect(parseTrackerCustomizationsImport("", [])).toEqual({ valid: false, error: null })
    expect(parseTrackerCustomizationsImport("{", [])).toEqual({ valid: false, error: "invalidJson" })
    expect(parseTrackerCustomizationsImport("{}", [])).toEqual({ valid: false, error: "invalidFormat" })
    expect(parseTrackerCustomizationsImport(JSON.stringify({ trackerCustomizations: [{ domains: ["a.example.invalid"] }] }), [])).toEqual({ valid: false, error: "missingDisplayName" })
    expect(parseTrackerCustomizationsImport(JSON.stringify({ trackerCustomizations: [{ displayName: "x", domains: [] }] }), [])).toEqual({ valid: false, error: "invalidDomains" })
    expect(parseTrackerCustomizationsImport(JSON.stringify({ trackerCustomizations: [{ displayName: "x", domains: [" "] }] }), [])).toEqual({ valid: false, error: "invalidDomains" })
  })
})

describe("buildTrackerRows", () => {
  it("folds the domains of a customization into one row and keeps a customization with no active domain", () => {
    const rows = buildTrackerRows(
      ["b.example.invalid", "a.example.invalid", "c.example.invalid"],
      [customization(1, "Merged", ["A.example.invalid", "b.example.invalid"]), customization(2, "Gone", ["z.example.invalid"])]
    )

    expect(rows.map(r => [r.displayName, r.domains, r.customizationId ?? null])).toEqual([
      ["c.example.invalid", ["c.example.invalid"], null],
      ["Gone", ["z.example.invalid"], 2],
      ["Merged", ["A.example.invalid", "b.example.invalid"], 1],
    ])
  })
})
