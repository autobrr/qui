/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expectTypeOf, it } from "vitest"
import type { FreeSpaceSource, FreeSpaceSourceType, ScoreRule, ScoreRuleType, SortingConfig, SortingType } from "@/types"

describe("rule discriminated unions", () => {
  it("use exactly the discriminants that the server accepts", () => {
    expectTypeOf<FreeSpaceSource["type"]>().toEqualTypeOf<FreeSpaceSourceType>()
    expectTypeOf<ScoreRule["type"]>().toEqualTypeOf<ScoreRuleType>()
    expectTypeOf<SortingConfig["type"]>().toEqualTypeOf<SortingType>()
  })
})
