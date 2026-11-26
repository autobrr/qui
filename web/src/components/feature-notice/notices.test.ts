/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { FEATURE_NOTICES } from "./notices"

describe("FEATURE_NOTICES", () => {
  it("holds three notices at most", () => {
    expect(FEATURE_NOTICES.length).toBeLessThanOrEqual(3)
  })
})
