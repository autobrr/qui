/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import type { BarWidth, ChartUnit } from "@/lib/statistics"
import { useTranslation } from "react-i18next"

/** Formats the start of a bar for a tooltip, and for an axis tick of a chart of unit. */
export function useBucketFormat(unit: ChartUnit) {
  const { i18n } = useTranslation()
  const locale = i18n.resolvedLanguage ?? i18n.language
  const hour12 = usePersistedDateTimePreferences().preferences.timeFormat === "12h"
  return {
    formatStart: (start: number, width: BarWidth) => width === "hour"
      ? new Date(start).toLocaleString(locale, { weekday: "short", hour: "2-digit", minute: "2-digit", hour12 })
      : new Date(start).toLocaleDateString(locale, width === "year" ? { year: "numeric" } : { day: "numeric", month: "short", year: "numeric" }),
    formatTick: (start: number, width?: BarWidth) => width === "year"
      ? new Date(start).toLocaleDateString(locale, { year: "numeric" })
      : unit === "hour"
        ? new Date(start).toLocaleString(locale, { weekday: "short", hour: "2-digit", hour12 })
        : new Date(start).toLocaleDateString(locale, { day: "numeric", month: "short" }),
  }
}
