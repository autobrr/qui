/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { useMemo } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { changeLanguage, initI18n } from "@/i18n"

beforeEach(async () => {
  localStorage.clear()
  localStorage.setItem("qui-datetime-preferences", JSON.stringify({
    timezone: "UTC",
    timeFormat: "24h",
    dateFormat: "relative",
  }))
  vi.spyOn(Date, "now").mockReturnValue(Date.UTC(2024, 5, 15, 15, 30, 45))
  await initI18n()
  await changeLanguage("en")
})

afterEach(async () => {
  cleanup()
  vi.restoreAllMocks()
  await changeLanguage("en")
  localStorage.clear()
})

describe("useDateTimeFormatters", () => {
  it("keeps callbacks stable when the language and preferences stay the same", () => {
    const { result, rerender } = renderHook(() => useDateTimeFormatters())
    const initial = result.current

    rerender()

    expect(result.current.formatTimestamp).toBe(initial.formatTimestamp)
    expect(result.current.formatDateOnly).toBe(initial.formatDateOnly)
    expect(result.current.formatTimeOnly).toBe(initial.formatTimeOnly)
    expect(result.current.formatDate).toBe(initial.formatDate)
    expect(result.current.formatAddedOn).toBe(initial.formatAddedOn)
    expect(result.current.formatISOTimestamp).toBe(initial.formatISOTimestamp)
  })

  it("updates a memoized date when the language changes", async () => {
    const { result } = renderHook(() => {
      const { formatTimestamp } = useDateTimeFormatters()
      // Table columns also use the formatter callback as a memo dependency.
      return useMemo(() => formatTimestamp(1718461845), [formatTimestamp])
    })
    expect(result.current).toBe("1 hour ago")

    await act(async () => {
      await changeLanguage("fr")
    })

    expect(result.current).toBe("il y a 1 heure")
  })
})
