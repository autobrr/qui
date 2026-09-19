/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { normalizeDirectoryPath } from "./paths"

describe("normalizeDirectoryPath", () => {
  it.each([
    ["", ""],
    ["/data", "/data/"],
    ["/data//", "/data/"],
    ["data", "/data/"],
    ["C:\\data", "C:\\data\\"],
    ["C:\\data\\", "C:\\data\\"],
    ["C:", "C:\\"],
    ["\\\\server\\share", "\\\\server\\share\\"],
  ])("%j -> %j", (input, expected) => {
    expect(normalizeDirectoryPath(input)).toBe(expected)
  })
})
