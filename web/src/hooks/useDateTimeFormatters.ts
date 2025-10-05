/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatAddedOn, formatDate, formatDateOnly, formatTimeOnly, formatTimestamp } from "@/lib/dateTimeUtils"

/**
 * Hook that provides date/time formatting functions that automatically use current user preferences
 * These functions will automatically update when preferences change
 */
export function useDateTimeFormatters() {
  const { preferences } = usePersistedDateTimePreferences()

  return {
    /**
     * Format a Unix timestamp (seconds) to a full date/time string
     */
    formatTimestamp: (timestamp: number) => formatTimestamp(timestamp, preferences),

    /**
     * Format a Unix timestamp (seconds) to a date-only string
     */
    formatDateOnly: (timestamp: number) => formatDateOnly(timestamp, preferences),

    /**
     * Format a Unix timestamp (seconds) to a time-only string
     */
    formatTimeOnly: (timestamp: number) => formatTimeOnly(timestamp, preferences),

    /**
     * Format a JavaScript Date object to a full date/time string
     */
    formatDate: (date: Date) => formatDate(date, preferences),

    /**
     * Format the "Added On" date for compatibility with existing components
     */
    formatAddedOn: (addedOn: number) => formatAddedOn(addedOn, preferences),

    /**
     * Get the current preferences (useful for conditional formatting)
     */
    preferences,
  }
}