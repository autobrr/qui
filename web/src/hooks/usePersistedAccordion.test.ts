/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { usePersistedAccordion } from "./usePersistedAccordion"

const DEFAULT_ITEMS = ["views", "status", "categories", "tags", "trackers"]

describe("usePersistedAccordion", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("returns defaults when nothing is stored", () => {
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(DEFAULT_ITEMS)
  })

  it("falls back to defaults and repairs storage on malformed JSON", () => {
    localStorage.setItem("qui-accordion", "{")
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(DEFAULT_ITEMS)
    expect(localStorage.getItem("qui-accordion")).toBe(JSON.stringify(DEFAULT_ITEMS))
  })

  it("seeds views into a pre-views stored array exactly once", () => {
    localStorage.setItem("qui-accordion", JSON.stringify(["status", "tags"]))
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(["views", "status", "tags"])
    expect(localStorage.getItem("qui-accordion-views-seeded")).toBe("1")
  })

  it("respects a collapsed views section once seeded", () => {
    localStorage.setItem("qui-accordion", JSON.stringify(["status", "tags"]))
    localStorage.setItem("qui-accordion-views-seeded", "1")
    const { result } = renderHook(() => usePersistedAccordion())
    expect(result.current[0]).toEqual(["status", "tags"])
  })
})
