/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatCalendarDay } from "@/lib/dateTimeUtils"

function preferences(dateFormat: DateTimePreferences["dateFormat"], timezone = "UTC"): DateTimePreferences {
  return { timezone, timeFormat: "24h", dateFormat }
}

describe("formatCalendarDay", () => {
  it("writes a day in the date format of the preferences, and in the ISO form for the relative format", () => {
    const day = new Date(2026, 1, 3, 12)

    expect(formatCalendarDay(day, preferences("iso"))).toBe("2026-02-03")
    expect(formatCalendarDay(day, preferences("eu"))).toBe("03/02/2026")
    expect(formatCalendarDay(day, preferences("us"))).toBe("02/03/2026")
    expect(formatCalendarDay(day, preferences("relative"))).toBe("2026-02-03")
  })

  it("keeps the day of the browser when the time zone of the preferences is on another day", () => {
    expect(formatCalendarDay(new Date(2026, 1, 3, 0, 30), preferences("iso", "Pacific/Pago_Pago"))).toBe("2026-02-03")
    expect(formatCalendarDay(new Date(2026, 1, 3, 23, 30), preferences("iso", "Pacific/Kiritimati"))).toBe("2026-02-03")
  })

  it("puts the short name of the weekday before the day", () => {
    expect(formatCalendarDay(new Date(2026, 1, 3, 12), preferences("iso"), "short")).toBe("Tue, 2026-02-03")
  })
})
