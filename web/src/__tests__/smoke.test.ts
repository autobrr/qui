/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Placeholder so the vitest pipeline runs in CI before real tests exist.
// Exercises jsdom, the @/* path alias, and explicit vitest imports — i.e.
// the toolchain pieces future tests will rely on. Delete this file once
// any other *.test.ts(x) covers the same surface.
import { cn } from "@/lib/utils"
import { describe, expect, it } from "vitest"

describe("vitest pipeline", () => {
  it("resolves the @/* alias and runs in a jsdom environment", () => {
    expect(cn("a", "b")).toContain("a")
    expect(typeof document).toBe("object")
  })
})
