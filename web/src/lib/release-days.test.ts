/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { releaseDays } from "./release-days"

const DAY = 86_400
const NOW = 20_000 * DAY + 3_600

describe("releaseDays", () => {
  it("puts each release of the last 52 weeks on its day", () => {
    const tags = [
      `v1.9.0 ${NOW - 10}`,
      `v1.8.1 ${NOW - 2 * DAY}`,
      `v1.8.0 ${NOW - 2 * DAY - 60}`,
      `v1.0.0 ${NOW - 363 * DAY}`,
      `v0.9.0 ${NOW - 364 * DAY}`,
      `v2.0.0-rc.1 ${NOW - 5 * DAY}`,
      `nightly ${NOW - 7 * DAY}`,
      "",
    ].join("\n")

    expect(releaseDays(tags, NOW)).toEqual([0, 361, 361, 363])
  })
})
