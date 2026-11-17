/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { AUTOMATIONS_TABS, automationsSearchSchema } from "./tabs"

describe("automations route search", () => {
  it("keeps every known tab value", () => {
    for (const tab of AUTOMATIONS_TABS) {
      expect(automationsSearchSchema.parse({ tab }).tab).toBe(tab)
    }
  })

  it("drops an unknown tab value so the page falls back to the default", () => {
    expect(automationsSearchSchema.parse({ tab: "orphan" }).tab).toBeUndefined()
    expect(automationsSearchSchema.parse({ tab: 3 }).tab).toBeUndefined()
    expect(automationsSearchSchema.parse({}).tab).toBeUndefined()
  })
})
