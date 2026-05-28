/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useTranslation } from "react-i18next"

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatAddedOn, formatDate, formatDateOnly, formatISOTimestamp, formatTimeOnly, formatTimestamp } from "@/lib/dateTimeUtils"

/**
 * Hook that provides date/time formatting functions that automatically use current user preferences
 * These functions will automatically update when preferences change
 */
export function useDateTimeFormatters() {
  const { preferences } = usePersistedDateTimePreferences()
  const { i18n } = useTranslation()
  const localeKey = i18n.resolvedLanguage || i18n.language || "en"

  return useMemo(() => {
    // The formatter utilities read the active locale from the i18n singleton.
    // Capture the current locale here so the memo invalidates when language changes.
    const activeLocale = localeKey

    return {
      /**
       * Format a Unix timestamp (seconds) to a full date/time string
       */
      formatTimestamp: (timestamp: number, includeSeconds = false) => {
        void activeLocale
        return formatTimestamp(timestamp, preferences, includeSeconds)
      },

      /**
       * Format a Unix timestamp (seconds) to a date-only string
       */
      formatDateOnly: (timestamp: number) => {
        void activeLocale
        return formatDateOnly(timestamp, preferences)
      },

      /**
       * Format a Unix timestamp (seconds) to a time-only string
       */
      formatTimeOnly: (timestamp: number, includeSeconds = false) => {
        void activeLocale
        return formatTimeOnly(timestamp, preferences, includeSeconds)
      },

      /**
       * Format a JavaScript Date object to a full date/time string
       */
      formatDate: (date: Date) => {
        void activeLocale
        return formatDate(date, preferences)
      },

      /**
       * Format the "Added On" date for compatibility with existing components
       */
      formatAddedOn: (addedOn: number) => {
        void activeLocale
        return formatAddedOn(addedOn, preferences)
      },

      /**
       * Format an ISO 8601 timestamp string (e.g., from activity logs)
       */
      formatISOTimestamp: (isoTimestamp: string) => {
        void activeLocale
        return formatISOTimestamp(isoTimestamp, preferences)
      },

      /**
       * Get the current preferences (useful for conditional formatting)
       */
      preferences,
    }
  }, [preferences, localeKey])
}
