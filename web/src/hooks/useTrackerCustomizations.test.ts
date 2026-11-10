/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import type { TrackerCustomization } from "@/types"
import { buildTrackerCustomizationMaps, trackerGroupKey } from "./useTrackerCustomizations"

function customization(id: number, displayName: string, domains: string[]): TrackerCustomization {
  return { id, displayName, domains, createdAt: "", updatedAt: "" }
}

function groupKeys(trackers: string[], customizations: TrackerCustomization[]): string[] {
  const { domainToCustomization } = buildTrackerCustomizationMaps(customizations)
  return trackers.map((tracker) => trackerGroupKey(tracker, domainToCustomization))
}

describe("trackerGroupKey", () => {
  it("keeps customizations apart when their display names differ only in case", () => {
    const keys = groupKeys(["ant.example", "other.example"], [
      customization(1, "ANT", ["ant.example"]),
      customization(2, "AnT", ["other.example"]),
    ])
    expect(new Set(keys).size).toBe(2)
  })

  it("keeps customizations apart when their display names are identical", () => {
    const keys = groupKeys(["a.example", "b.example"], [
      customization(1, "Tracker", ["a.example"]),
      customization(2, "Tracker", ["b.example"]),
    ])
    expect(new Set(keys).size).toBe(2)
  })

  it("groups every domain of one customization together", () => {
    const keys = groupKeys(["a.example", "B.example"], [customization(1, "Merged", ["a.example", "b.example"])])
    expect(new Set(keys).size).toBe(1)
  })

  it("keeps an uncustomized domain apart from a customization named after it", () => {
    const keys = groupKeys(["named.example", "plain.example"], [customization(1, "plain.example", ["named.example"])])
    expect(new Set(keys).size).toBe(2)
  })

  it("groups uncustomized domains case-insensitively", () => {
    const keys = groupKeys(["Plain.example", "plain.example"], [])
    expect(new Set(keys).size).toBe(1)
  })
})
