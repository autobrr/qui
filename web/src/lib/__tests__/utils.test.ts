/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseTrackerDomains } from "@/lib/utils"
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
