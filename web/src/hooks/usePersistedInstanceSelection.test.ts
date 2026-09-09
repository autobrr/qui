/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { resolveInstanceSelection } from "@/hooks/usePersistedInstanceSelection"

const instances = [
  { id: 101, connected: false },
  { id: 202, connected: true },
]

describe("resolveInstanceSelection", () => {
  it("keeps the saved selection while instances are still loading", () => {
    // The instances query resolving later must not clear a saved id; clearing
    // here persisted the "" sentinel and the fallback then overwrote the
    // saved selection on every reload.
    expect(resolveInstanceSelection(undefined, 101)).toBe(101)
    expect(resolveInstanceSelection(undefined, undefined)).toBeUndefined()
  })

  it("clears the selection when the instance list is confirmed empty", () => {
    expect(resolveInstanceSelection([], 101)).toBeUndefined()
  })

  it("keeps a saved selection that exists, connected or not", () => {
    expect(resolveInstanceSelection(instances, 101)).toBe(101)
    expect(resolveInstanceSelection(instances, 202)).toBe(202)
  })

  it("falls back to the first connected instance when the saved id is gone", () => {
    expect(resolveInstanceSelection(instances, 999)).toBe(202)
  })

  it("picks the first connected instance when nothing is saved", () => {
    expect(resolveInstanceSelection(instances, undefined)).toBe(202)
  })

  it("picks the first instance when none are connected", () => {
    const disconnected = [{ id: 101, connected: false }, { id: 303, connected: false }]
    expect(resolveInstanceSelection(disconnected, undefined)).toBe(101)
    expect(resolveInstanceSelection(disconnected, 999)).toBe(101)
  })
})
