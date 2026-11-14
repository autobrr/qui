/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { endsWithSeparator, lastSeparatorIndex, normalizeDirectoryPath, pathSeparator } from "./paths"

describe("pathSeparator", () => {
  it.each([
    ["/data/movies", "/"],
    ["C:\\data", "\\"],
    ["\\\\server\\share", "\\"],
    // A drive letter or UNC prefix later in a Unix path does not make it a Windows path.
    ["/data/C:/movies", "/"],
    ["/data/\\\\share", "/"],
  ])("%j -> %j", (input, expected) => {
    expect(pathSeparator(input)).toBe(expected)
  })
})

describe("lastSeparatorIndex", () => {
  it.each([
    ["/data/movies", 5],
    ["/data/ac\\dc", 5],
    ["C:\\data\\movies", 7],
    ["C:\\data/movies", 7],
    ["movies", -1],
  ])("%j -> %j", (input, expected) => {
    expect(lastSeparatorIndex(input)).toBe(expected)
  })
})

describe("endsWithSeparator", () => {
  it.each([
    ["", false],
    ["/data/", true],
    ["/data", false],
    ["/data/ac\\", false],
    ["C:\\data\\", true],
    ["C:\\data/", true],
    ["C:\\data", false],
  ])("%j -> %j", (input, expected) => {
    expect(endsWithSeparator(input)).toBe(expected)
  })
})

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
    ["/data/ac\\dc", "/data/ac\\dc/"],
    ["/data/ac\\dc/", "/data/ac\\dc/"],
  ])("%j -> %j", (input, expected) => {
    expect(normalizeDirectoryPath(input)).toBe(expected)
  })
})
