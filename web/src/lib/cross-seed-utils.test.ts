/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { changedSecret } from "./cross-seed-utils"

describe("changedSecret", () => {
  it.each([
    { name: "untouched unset key", value: "", saved: undefined, want: undefined },
    { name: "untouched set key", value: "<redacted>", saved: "<redacted>", want: undefined },
    { name: "cleared key", value: "", saved: "<redacted>", want: "" },
    { name: "new key", value: "new-key", saved: undefined, want: "new-key" },
  ])("$name", ({ value, saved, want }) => {
    expect(changedSecret(value, saved)).toBe(want)
  })
})
