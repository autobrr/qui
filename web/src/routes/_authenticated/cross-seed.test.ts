/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it, vi } from "vitest"

vi.mock("@/pages/CrossSeedPage", () => ({ CrossSeedPage: () => null }))

import { Route } from "./cross-seed"

function validate(search: Record<string, unknown>) {
  // validateSearch is the zod schema itself; the router calls it as a standard schema.
  const schema = Route.options.validateSearch as { parse: (input: unknown) => { tab?: string } }
  return schema.parse(search).tab
}

describe("cross-seed route search", () => {
  it("keeps every known tab value", () => {
    for (const tab of ["rss", "webhook", "library", "directories", "season-packs", "rules", "blocklist"]) {
      expect(validate({ tab })).toBe(tab)
    }
  })

  it("drops an unknown or retired tab value so the page falls back to rss", () => {
    expect(validate({ tab: "auto" })).toBeUndefined()
    expect(validate({ tab: "scan" })).toBeUndefined()
    expect(validate({})).toBeUndefined()
  })
})
