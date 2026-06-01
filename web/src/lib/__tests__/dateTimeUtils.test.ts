/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import {
  formatAddedOn,
  formatDate,
  formatDateOnly,
  formatISOTimestamp,
  formatRelativeTime,
  formatTimeHMS,
  formatTimeOnly,
  formatTimestamp
} from "@/lib/dateTimeUtils"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Fixed reference point used by every formatter test below. Picking a known
// timestamp lets us assert exact output strings rather than approximate
// substrings. 2024-06-15 14:30:45 UTC -> 1718461845.
const TS_2024_06_15_14_30_45_UTC = 1718461845

const originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, "localStorage")

const utcPrefs = (overrides: Partial<DateTimePreferences> = {}): DateTimePreferences => ({
  timezone: "UTC",
  timeFormat: "24h",
  dateFormat: "iso",
  ...overrides,
})

const createStorageStub = (): Storage => {
  const entries = new Map<string, string>()

  return {
    get length() {
      return entries.size
    },
    clear: () => {
      entries.clear()
    },
    getItem: (key: string) => entries.get(key) ?? null,
    key: (index: number) => Array.from(entries.keys())[index] ?? null,
    removeItem: (key: string) => {
      entries.delete(key)
    },
    setItem: (key: string, value: string) => {
      entries.set(key, value)
    },
  }
}

// Node 26 exposes an experimental global localStorage accessor that returns
// undefined unless Node is started with --localstorage-file. Keep this test on
// the browser Storage contract without depending on runtime-specific globals.
const installTestLocalStorage = () => {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: createStorageStub(),
  })
}

const restoreOriginalLocalStorage = () => {
  if (originalLocalStorageDescriptor) {
    Object.defineProperty(globalThis, "localStorage", originalLocalStorageDescriptor)
    return
  }

  Reflect.deleteProperty(globalThis, "localStorage")
}

// Intent: Unix-seconds timestamp -> formatted string using the user's
// timezone + date/time format preferences. Tests pass explicit preferences
// so we never depend on localStorage; the localStorage fallback path is
// covered separately.
describe("formatTimestamp", () => {
  it.each([0, undefined as unknown as number, null as unknown as number])("returns 'N/A' for falsy timestamp %p", (ts) => {
    expect(formatTimestamp(ts)).toBe("N/A")
  })

  it("formats with iso date format in 24h", () => {
    expect(formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs())).toBe("2024-06-15 14:30")
  })

  it("respects the includeSeconds flag", () => {
    expect(formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs(), true)).toBe("2024-06-15 14:30:45")
  })

  it("respects 12h time format (hour12)", () => {
    const result = formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs({ timeFormat: "12h" }))
    // iso date + 12h time. Asserting structurally rather than exact whitespace
    // because Intl uses U+202F (narrow no-break space) before AM/PM in some
    // engines.
    expect(result).toMatch(/^2024-06-15 02:30\s*PM$/)
  })

  it("respects timezone (UTC vs America/New_York)", () => {
    const utc = formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs())
    const ny = formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs({ timezone: "America/New_York" }))
    expect(utc).toBe("2024-06-15 14:30")
    expect(ny).toBe("2024-06-15 10:30") // EDT is UTC-4 in June
  })

  it("formats with us date format (MM/DD/YYYY)", () => {
    const result = formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "us" }))
    expect(result).toContain("06/15/2024")
  })

  it("formats with eu date format (DD/MM/YYYY)", () => {
    const result = formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "eu" }))
    expect(result).toContain("15/06/2024")
  })

  it("relative format delegates to formatRelativeTime", () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2024-06-15T14:30:50Z")) // 5 seconds after
      expect(formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "relative" }))).toBe("Just now")
    } finally {
      vi.useRealTimers()
    }
  })
})

// Intent: same machinery as formatTimestamp but date-only — no time
// component. The relative format still applies (date-only relative makes
// sense for "added 2 days ago" style displays).
describe("formatDateOnly", () => {
  it("returns 'N/A' for falsy timestamp", () => {
    expect(formatDateOnly(0)).toBe("N/A")
  })

  it("formats iso date without time", () => {
    expect(formatDateOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs())).toBe("2024-06-15")
  })

  it("formats us date without time", () => {
    expect(formatDateOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "us" }))).toBe("06/15/2024")
  })

  it("formats eu date without time", () => {
    expect(formatDateOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "eu" }))).toBe("15/06/2024")
  })

  it("returns a relative day label for relative format", () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date("2024-06-15T14:30:50Z"))
      expect(formatDateOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ dateFormat: "relative" }))).toBe("Today")
    } finally {
      vi.useRealTimers()
    }
  })
})

// Intent: time-only formatter. Uses the user's timezone and 12h/24h
// preference. Locale comes from the runtime ([] arg), so we assert
// structurally on the parts we control.
describe("formatTimeOnly", () => {
  it("returns 'N/A' for falsy timestamp", () => {
    expect(formatTimeOnly(0)).toBe("N/A")
  })

  it("respects 24h vs 12h preference under the same timezone", () => {
    const tf24 = formatTimeOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ timeFormat: "24h" }))
    const tf12 = formatTimeOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ timeFormat: "12h" }))
    expect(tf24).toMatch(/^14:30$/)
    expect(tf12).toMatch(/^02:30\s*PM$/)
  })

  it("includes seconds when requested", () => {
    expect(formatTimeOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs(), true)).toMatch(/^14:30:45$/)
  })

  it("shifts time by timezone", () => {
    expect(formatTimeOnly(TS_2024_06_15_14_30_45_UTC, utcPrefs({ timezone: "America/New_York" }))).toMatch(/^10:30$/)
  })
})

// Intent: thin Date -> formatted-string wrapper. Just confirm it routes
// through formatTimestamp with the right epoch conversion.
describe("formatDate", () => {
  it("converts a Date to seconds and delegates to formatTimestamp", () => {
    const d = new Date("2024-06-15T14:30:45Z")
    expect(formatDate(d, utcPrefs())).toBe("2024-06-15 14:30")
  })
})

// Intent: alias of formatTimestamp used by the torrent table for the
// "Added On" column. Keep the contract obvious so callers don't drift.
describe("formatAddedOn", () => {
  it("is equivalent to formatTimestamp", () => {
    expect(formatAddedOn(TS_2024_06_15_14_30_45_UTC, utcPrefs())).toBe(
      formatTimestamp(TS_2024_06_15_14_30_45_UTC, utcPrefs())
    )
  })
})

// Intent: parse an ISO string and format it. Two safety properties matter:
// 1) bad input falls back to the original string instead of throwing, and
// 2) empty input returns "N/A" (same convention as the unix-seconds APIs).
describe("formatISOTimestamp", () => {
  it("returns 'N/A' for empty string", () => {
    expect(formatISOTimestamp("")).toBe("N/A")
  })

  it("formats a valid ISO 8601 string", () => {
    expect(formatISOTimestamp("2024-06-15T14:30:45Z", utcPrefs())).toBe("2024-06-15 14:30")
  })

  it("returns the original string when the input cannot be parsed", () => {
    expect(formatISOTimestamp("not-a-date", utcPrefs())).toBe("not-a-date")
  })
})

// Intent: always relative, independent of user preferences. Drives "last
// activity", "seen complete", and similar at-a-glance fields. Must handle
// past + future + invalid inputs without throwing.
describe("formatRelativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2024-06-15T12:00:00Z"))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    [null, "—"],
    [undefined, "—"],
    ["not-a-date", "—"],
    [NaN, "—"],
  ])("returns em-dash for invalid input %p", (input, expected) => {
    expect(formatRelativeTime(input as Parameters<typeof formatRelativeTime>[0])).toBe(expected)
  })

  it("returns 'Just now' for the present and the recent past (no suffix)", () => {
    expect(formatRelativeTime(new Date("2024-06-15T12:00:00Z"))).toBe("Just now")
    expect(formatRelativeTime(new Date("2024-06-15T11:59:30Z"))).toBe("Just now")
  })

  it("formats past intervals with 'ago' suffix", () => {
    expect(formatRelativeTime(new Date("2024-06-15T11:30:00Z"))).toBe("30 minutes ago")
    expect(formatRelativeTime(new Date("2024-06-15T10:00:00Z"))).toBe("2 hours ago")
    expect(formatRelativeTime(new Date("2024-06-13T12:00:00Z"))).toBe("2 days ago")
    expect(formatRelativeTime(new Date("2024-05-25T12:00:00Z"))).toBe("3 weeks ago")
    expect(formatRelativeTime(new Date("2024-03-15T12:00:00Z"))).toBe("3 months ago")
    expect(formatRelativeTime(new Date("2022-06-15T12:00:00Z"))).toBe("2 years ago")
  })

  it("formats future intervals with 'in' prefix", () => {
    expect(formatRelativeTime(new Date("2024-06-15T12:30:00Z"))).toBe("in 30 minutes")
    expect(formatRelativeTime(new Date("2024-06-16T12:00:00Z"))).toBe("in 1 day")
  })

  it("uses singular vs plural correctly", () => {
    expect(formatRelativeTime(new Date("2024-06-15T11:59:00Z"))).toBe("1 minute ago")
    expect(formatRelativeTime(new Date("2024-06-15T11:00:00Z"))).toBe("1 hour ago")
    expect(formatRelativeTime(new Date("2024-06-14T12:00:00Z"))).toBe("1 day ago")
  })

  it("omits suffix when addSuffix=false", () => {
    expect(formatRelativeTime(new Date("2024-06-15T10:00:00Z"), false)).toBe("2 hours")
  })

  it("accepts Unix seconds (number)", () => {
    expect(formatRelativeTime(1718445600)).toBe("2 hours ago") // 2024-06-15T10:00:00Z
  })

  it("accepts ISO string", () => {
    expect(formatRelativeTime("2024-06-15T10:00:00Z")).toBe("2 hours ago")
  })
})

// Intent: HH:mm:ss using the host's LOCAL timezone. Pin behavior by
// constructing the Date in local time so the test runs deterministically
// regardless of CI vs developer-machine timezone.
describe("formatTimeHMS", () => {
  it("zero-pads hours, minutes, seconds", () => {
    // Local-time date constructor — values come out the way we put them in,
    // regardless of test runner's timezone.
    expect(formatTimeHMS(new Date(2024, 0, 15, 4, 5, 9))).toBe("04:05:09")
    expect(formatTimeHMS(new Date(2024, 0, 15, 14, 30, 45))).toBe("14:30:45")
    expect(formatTimeHMS(new Date(2024, 0, 15, 0, 0, 0))).toBe("00:00:00")
    expect(formatTimeHMS(new Date(2024, 0, 15, 23, 59, 59))).toBe("23:59:59")
  })
})

// Intent: when no preferences are passed, the formatter pulls from
// localStorage. Empty / missing storage uses a sensible default. Errors
// during read are swallowed (logged) so a corrupted key never crashes
// the UI.
describe("formatTimestamp localStorage fallback", () => {
  beforeEach(() => {
    installTestLocalStorage()
    localStorage.clear()
  })

  afterEach(() => {
    restoreOriginalLocalStorage()
  })

  it("uses the runtime's resolved timezone when localStorage is empty", () => {
    // We can't assert an exact value (depends on test runner) but the call
    // must succeed and return a non-empty, non-"N/A" string.
    const result = formatTimestamp(TS_2024_06_15_14_30_45_UTC)
    expect(result).not.toBe("N/A")
    expect(result.length).toBeGreaterThan(0)
  })

  it("honors stored preferences", () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({
      timezone: "UTC",
      timeFormat: "24h",
      dateFormat: "iso",
    }))
    expect(formatTimestamp(TS_2024_06_15_14_30_45_UTC)).toBe("2024-06-15 14:30")
  })

  it("falls back gracefully when localStorage holds invalid JSON", () => {
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {})
    try {
      localStorage.setItem("qui-datetime-preferences", "not-valid-json")
      const result = formatTimestamp(TS_2024_06_15_14_30_45_UTC)
      expect(result).not.toBe("N/A")
      expect(errSpy).toHaveBeenCalled()
    } finally {
      errSpy.mockRestore()
    }
  })
})
