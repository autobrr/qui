/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { formatErrorMessage } from "@/lib/format-error-message"

// formatErrorMessage imports "@/i18n", which initializes English synchronously,
// so "Unknown error" resolves here without any test setup.

describe("formatErrorMessage", () => {
  it.each([
    ["undefined", undefined],
    ["empty", ""],
    ["whitespace", "   "],
  ])("returns the translated unknown-error text for %s input", (_label, input) => {
    expect(formatErrorMessage(input)).toBe("Unknown error")
  })

  it.each([
    ["failed to create client: bad credentials", "Bad credentials"],
    ["failed to connect to qBittorrent instance: refused", "Refused"],
    ["connection failed: timeout", "Timeout"],
    ["Error: disk is full", "Disk is full"],
  ])("strips the %s prefix and capitalizes the rest", (input, expected) => {
    expect(formatErrorMessage(input)).toBe(expected)
  })

  it("keeps a message that carries no known prefix", () => {
    expect(formatErrorMessage("tracker returned 403")).toBe("Tracker returned 403")
  })

  // The prefixes end in a space, and the input is trimmed first, so a bare
  // prefix keeps its own text rather than stripping to nothing.
  it("keeps a bare prefix as the message", () => {
    expect(formatErrorMessage("connection failed: ")).toBe("Connection failed:")
  })
})
