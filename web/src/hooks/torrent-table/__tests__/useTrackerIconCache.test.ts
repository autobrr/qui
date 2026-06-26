/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTrackerIconCache } from "@/hooks/torrent-table/useTrackerIconCache"
import type { TrackerCustomization } from "@/types"
import { renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: vi.fn() }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: vi.fn() }))

import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"

const mockIcons = vi.mocked(useTrackerIcons)
const mockCustomizations = vi.mocked(useTrackerCustomizations)

function iconsResult(data: Record<string, string> | undefined) {
  return { data } as unknown as ReturnType<typeof useTrackerIcons>
}

function customizationsResult(data: TrackerCustomization[] | undefined) {
  return { data } as unknown as ReturnType<typeof useTrackerCustomizations>
}

function makeCustomization(overrides: Partial<TrackerCustomization> = {}): TrackerCustomization {
  return {
    id: 1,
    displayName: "Tracker",
    domains: ["tracker.example.com"],
    createdAt: "2024-01-01T00:00:00Z",
    updatedAt: "2024-01-01T00:00:00Z",
    ...overrides,
  }
}

beforeEach(() => {
  mockIcons.mockReturnValue(iconsResult(undefined))
  mockCustomizations.mockReturnValue(customizationsResult(undefined))
})

afterEach(() => {
  vi.clearAllMocks()
})

describe("useTrackerIconCache", () => {
  it("returns undefined icons and an empty lookup before any data arrives", () => {
    const { result } = renderHook(() => useTrackerIconCache())
    expect(result.current.trackerIcons).toBeUndefined()
    expect(result.current.trackerCustomizationLookup.size).toBe(0)
  })

  it("keeps a stable icon reference when new data has identical contents", () => {
    mockIcons.mockReturnValue(iconsResult({ "a.com": "iconA" }))
    const { result, rerender } = renderHook(() => useTrackerIconCache())
    const first = result.current.trackerIcons
    expect(first).toEqual({ "a.com": "iconA" })

    // New object, same contents → shallow-equal → previous reference retained.
    mockIcons.mockReturnValue(iconsResult({ "a.com": "iconA" }))
    rerender()
    expect(result.current.trackerIcons).toBe(first)
  })

  it("returns a new icon reference when contents change", () => {
    mockIcons.mockReturnValue(iconsResult({ "a.com": "iconA" }))
    const { result, rerender } = renderHook(() => useTrackerIconCache())
    const first = result.current.trackerIcons

    mockIcons.mockReturnValue(iconsResult({ "a.com": "iconB" }))
    rerender()
    expect(result.current.trackerIcons).not.toBe(first)
    expect(result.current.trackerIcons).toEqual({ "a.com": "iconB" })
  })

  it("retains the last icons when data becomes undefined", () => {
    mockIcons.mockReturnValue(iconsResult({ "a.com": "iconA" }))
    const { result, rerender } = renderHook(() => useTrackerIconCache())
    const first = result.current.trackerIcons

    mockIcons.mockReturnValue(iconsResult(undefined))
    rerender()
    expect(result.current.trackerIcons).toBe(first)
  })

  it("reuses the customization lookup until the cache key changes", () => {
    mockCustomizations.mockReturnValue(customizationsResult([makeCustomization()]))
    const { result, rerender } = renderHook(() => useTrackerIconCache())
    const first = result.current.trackerCustomizationLookup

    // New array, same id + updatedAt → same cache key → same lookup reference.
    mockCustomizations.mockReturnValue(customizationsResult([makeCustomization()]))
    rerender()
    expect(result.current.trackerCustomizationLookup).toBe(first)

    // Changed updatedAt → new cache key → rebuilt lookup.
    mockCustomizations.mockReturnValue(customizationsResult([makeCustomization({ updatedAt: "2024-02-02T00:00:00Z" })]))
    rerender()
    expect(result.current.trackerCustomizationLookup).not.toBe(first)
  })
})
