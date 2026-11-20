/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
import type { BarWidth, ChartUnit } from "@/lib/statistics"
import { useTranslation } from "react-i18next"

/** Formats the start of a bar for a tooltip, and for an axis tick of a chart of unit. */
export function useBucketFormat(unit: ChartUnit) {
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
    formatTick: (start: number, width?: BarWidth) => width === "year"
      ? new Date(start).toLocaleDateString(locale, { year: "numeric" })
      : width === "month"
        ? new Date(start).toLocaleDateString(locale, { month: "short", year: "2-digit" })
        : unit === "hour"
          ? new Date(start).toLocaleString(locale, { weekday: "short", hour: "2-digit", hour12 })
          : new Date(start).toLocaleDateString(locale, { day: "numeric", month: "short" }),
  }
}
