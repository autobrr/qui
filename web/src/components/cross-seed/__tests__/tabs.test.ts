/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { CROSS_SEED_TABS, crossSeedSearchSchema } from "../tabs"

describe("cross-seed route search", () => {
  it("keeps every known tab value", () => {
    for (const tab of CROSS_SEED_TABS) {
      expect(crossSeedSearchSchema.parse({ tab }).tab).toBe(tab)
    }
  })

  it("drops an unknown or retired tab value so the page falls back to the default", () => {
    expect(crossSeedSearchSchema.parse({ tab: "auto" }).tab).toBeUndefined()
    expect(crossSeedSearchSchema.parse({ tab: "scan" }).tab).toBeUndefined()
    expect(crossSeedSearchSchema.parse({ tab: "rules" }).tab).toBeUndefined()
    expect(crossSeedSearchSchema.parse({}).tab).toBeUndefined()
  })
})
