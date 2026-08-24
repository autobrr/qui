/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { usePersistedAccordion } from "./usePersistedAccordion"

const DEFAULT_ITEMS = ["views", "status", "categories", "tags", "trackers"]

describe("usePersistedAccordion", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("returns defaults when nothing is stored", () => {
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(DEFAULT_ITEMS)
  })

  it("falls back to defaults on malformed JSON and repairs storage on the next toggle", () => {
    localStorage.setItem("qui-accordion", "{")
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(DEFAULT_ITEMS)

    act(() => result.current[1](["status"]))

    expect(localStorage.getItem("qui-accordion")).toBe(JSON.stringify(["status"]))
  })

  it("falls back to defaults on valid JSON of the wrong shape, even when seeded", () => {
    localStorage.setItem("qui-accordion", "5")
    localStorage.setItem("qui-accordion-views-seeded", "1")
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(DEFAULT_ITEMS)
  })

  it("seeds views into a pre-views stored array, marking seeded on the first toggle", () => {
    localStorage.setItem("qui-accordion", JSON.stringify(["status", "tags"]))
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(["views", "status", "tags"])

    act(() => result.current[1](prev => prev.filter(item => item !== "tags")))

    expect(result.current[0]).toEqual(["views", "status"])
    expect(localStorage.getItem("qui-accordion-views-seeded")).toBe("1")
  })

  it("respects a collapsed views section once seeded", () => {
    localStorage.setItem("qui-accordion", JSON.stringify(["status", "tags"]))
    localStorage.setItem("qui-accordion-views-seeded", "1")
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(["status", "tags"])
  })

  it("survives blocked storage writes instead of taking the sidebar down", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("QuotaExceededError")
    })
    vi.spyOn(console, "error").mockImplementation(() => {})

    const { result } = renderHook(() => usePersistedAccordion())

    expect(result.current[0]).toEqual(DEFAULT_ITEMS)
  })
})
