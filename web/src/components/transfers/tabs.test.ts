/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { TRANSFERS_TABS, transfersSearchSchema } from "./tabs"

describe("transfers route search", () => {
  it("keeps every known tab value", () => {
    for (const tab of TRANSFERS_TABS) {
      expect(transfersSearchSchema.parse({ tab }).tab).toBe(tab)
    }
  })

  it("drops an unknown tab value so the page falls back to the default", () => {
    expect(transfersSearchSchema.parse({ tab: "share" }).tab).toBeUndefined()
    expect(transfersSearchSchema.parse({ tab: 3 }).tab).toBeUndefined()
    expect(transfersSearchSchema.parse({}).tab).toBeUndefined()
  })
})
