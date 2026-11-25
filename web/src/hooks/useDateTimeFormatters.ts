/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useTranslation } from "react-i18next"

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatClockTime, formatDate, formatDateOnly, formatISOTimestamp, formatTimeOnly, formatTimestamp } from "@/lib/dateTimeUtils"

export function useDateTimeFormatters() {
  const { preferences } = usePersistedDateTimePreferences()
  const { i18n } = useTranslation()
  const localeKey = i18n.resolvedLanguage || i18n.language || "en"

  return useMemo(() => {
    const activeLocale = localeKey

    return {
      formatTimestamp: (timestamp: number, includeSeconds = false) => {
        void activeLocale
        return formatTimestamp(timestamp, preferences, includeSeconds)
      },

      formatDateOnly: (timestamp: number) => {
        void activeLocale
        return formatDateOnly(timestamp, preferences)
      },

      formatTimeOnly: (timestamp: number, includeSeconds = false) => {
        void activeLocale
        return formatTimeOnly(timestamp, preferences, includeSeconds)
      },

      formatDate: (date: Date) => {
        void activeLocale
        return formatDate(date, preferences)
      },

      formatClockTime: (date: Date) => {
        void activeLocale
        return formatClockTime(date, preferences)
      },

      formatISOTimestamp: (isoTimestamp: string) => {
        void activeLocale
        return formatISOTimestamp(isoTimestamp, preferences)
      },

      preferences,
    }
  }, [preferences, localeKey])
}
