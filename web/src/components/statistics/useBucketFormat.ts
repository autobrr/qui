/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
import type { BarWidth } from "@/lib/statistics"
import { useTranslation } from "react-i18next"

export function useBucketFormat() {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { preferences } = usePersistedDateTimePreferences()
  const hour12 = preferences.timeFormat === "12h"
  return {
    formatStart: (start: number, width: BarWidth) => {
      const date = new Date(start)
      switch (width) {
        case "hour":
          return date.toLocaleString(locale, { weekday: "short", hour: "2-digit", minute: "2-digit", hour12 })
        case "week":
          // The first and the last week of a period can be partial, so the label names the Monday, not a range.
          return t("statistics:chart.weekOf", { date: formatCalendarDay(date, preferences) })
        case "month":
          return date.toLocaleDateString(locale, { month: "long", year: "numeric" })
        case "year":
          return date.toLocaleDateString(locale, { year: "numeric" })
        default:
          return formatCalendarDay(date, preferences)
      }
    },
    formatHour: (start: number) => new Date(start).toLocaleTimeString(locale, hour12 ? { hour: "numeric", hour12 } : { hour: "2-digit", minute: "2-digit", hour12 }),
    formatDayName: (day: number, withDate: boolean) => new Date(day).toLocaleDateString(locale, withDate ? { weekday: "short", day: "numeric", month: "short" } : { weekday: "short" }),
    formatTick: (start: number, width?: BarWidth) => width === "year"
      ? new Date(start).toLocaleDateString(locale, { year: "numeric" })
      : width === "month"
        ? new Date(start).toLocaleDateString(locale, { month: "short", year: "2-digit" })
        : new Date(start).toLocaleDateString(locale, { day: "numeric", month: "short" }),
  }
}
