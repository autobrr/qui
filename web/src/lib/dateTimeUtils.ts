/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import i18n, { normalizeLanguage } from "@/i18n"

const SECOND_MS = 1000
const MINUTE_MS = 60 * SECOND_MS
const HOUR_MS = 60 * MINUTE_MS
const DAY_MS = 24 * HOUR_MS
const WEEK_MS = 7 * DAY_MS
const MONTH_MS = 30 * DAY_MS
const YEAR_MS = 365 * DAY_MS

type RelativeFormatterStyle = "long" | "short"
type RelativeUnit = Intl.RelativeTimeFormatUnit

function getRelativeLocale(): string {
  if (i18n.resolvedLanguage || i18n.language) {
    return normalizeLanguage(i18n.resolvedLanguage || i18n.language)
  }

  if (typeof navigator !== "undefined") {
    return normalizeLanguage(navigator.language)
  }

  return "en"
}

function getRelativeParts(diffMs: number): { value: number; unit: RelativeUnit } {
  const absDiffMs = Math.abs(diffMs)

  if (absDiffMs < MINUTE_MS) {
    return { value: Math.round(diffMs / SECOND_MS), unit: "second" }
  }

  if (absDiffMs < HOUR_MS) {
    return { value: Math.round(diffMs / MINUTE_MS), unit: "minute" }
  }

  if (absDiffMs < DAY_MS) {
    return { value: Math.round(diffMs / HOUR_MS), unit: "hour" }
  }

  if (absDiffMs < WEEK_MS) {
    return { value: Math.round(diffMs / DAY_MS), unit: "day" }
  }

  if (absDiffMs < MONTH_MS) {
    return { value: Math.round(diffMs / WEEK_MS), unit: "week" }
  }

  if (absDiffMs < YEAR_MS) {
    return { value: Math.round(diffMs / MONTH_MS), unit: "month" }
  }

  return { value: Math.round(diffMs / YEAR_MS), unit: "year" }
}

function formatRelativeParts(
  value: number,
  unit: RelativeUnit,
  {
    locale = getRelativeLocale(),
    style = "long",
    withDirection = true,
  }: {
    locale?: string
    style?: RelativeFormatterStyle
    withDirection?: boolean
  } = {}
): string {
  if (withDirection) {
    return new Intl.RelativeTimeFormat(locale, {
      numeric: "auto",
      style,
    }).format(value, unit)
  }

  return new Intl.NumberFormat(locale, {
    style: "unit",
    unit,
    unitDisplay: style === "short" ? "short" : "long",
  }).format(Math.abs(value))
}

export function formatLocalizedRelativeTime(
  date: Date,
  {
    locale = getRelativeLocale(),
    style = "long",
    withDirection = true,
  }: {
    locale?: string
    style?: RelativeFormatterStyle
    withDirection?: boolean
  } = {}
): string {
  const { value, unit } = getRelativeParts(date.getTime() - Date.now())
  return formatRelativeParts(value, unit, { locale, style, withDirection })
}

// Get stored preferences from localStorage
function getStoredPreferences(): DateTimePreferences {
  try {
    const stored = localStorage.getItem("qui-datetime-preferences")
    if (stored) {
      const parsed = JSON.parse(stored)
      return {
        timezone: parsed.timezone || "UTC",
        timeFormat: parsed.timeFormat || "24h",
        dateFormat: parsed.dateFormat || "iso",
      }
    }
  } catch (error) {
    console.error("Failed to load date/time preferences:", error)
  }

  // Fallback to defaults
  return {
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    timeFormat: "24h",
    dateFormat: "iso",
  }
}

/**
 * Format a timestamp using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date/time string
 */
export function formatTimestamp(timestamp: number, preferences?: DateTimePreferences): string {
  if (!timestamp || timestamp === 0) return "N/A"

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  // For relative format, return relative time
  if (prefs.dateFormat === "relative") {
    return formatLocalizedRelativeTime(date)
  }

  try {
    const timeZone = prefs.timezone
    const hour12 = prefs.timeFormat === "12h"

    switch (prefs.dateFormat) {
      case "iso": {
        // ISO 8601 format: YYYY-MM-DD HH:MM covering the preferred timezone
        const dateFormatter = new Intl.DateTimeFormat("en-CA", {
          timeZone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
        })
        const timeFormatter = new Intl.DateTimeFormat("en-US", {
          timeZone,
          hour: "2-digit",
          minute: "2-digit",
          hour12,
        })
        return `${dateFormatter.format(date)} ${timeFormatter.format(date)}`
      }

      case "us": {
        // US format: MM/DD/YYYY HH:MM AM/PM
        return date.toLocaleString("en-US", {
          timeZone,
          month: "2-digit",
          day: "2-digit",
          year: "numeric",
          hour: "2-digit",
          minute: "2-digit",
          hour12,
        })
      }

      case "eu": {
        // European format: DD/MM/YYYY HH:MM
        return date.toLocaleString("en-GB", {
          timeZone,
          day: "2-digit",
          month: "2-digit",
          year: "numeric",
          hour: "2-digit",
          minute: "2-digit",
          hour12,
        })
      }

      default: {
        // Fallback to ISO format
        const dateFormatter = new Intl.DateTimeFormat("en-CA", {
          timeZone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
        })
        const timeFormatter = new Intl.DateTimeFormat("en-US", {
          timeZone,
          hour: "2-digit",
          minute: "2-digit",
          hour12,
        })
        return `${dateFormatter.format(date)} ${timeFormatter.format(date)}`
      }
    }
  } catch (error) {
    console.error("Error formatting timestamp:", error)
    // Fallback to basic formatting
    return new Date(timestamp * 1000).toLocaleString()
  }
}

/**
 * Format a date only (without time) using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date string
 */
export function formatDateOnly(timestamp: number, preferences?: DateTimePreferences): string {
  if (!timestamp || timestamp === 0) return "N/A"

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  // For relative format, return relative date
  if (prefs.dateFormat === "relative") {
    return formatLocalizedRelativeTime(date)
  }

  try {
    const timeZone = prefs.timezone

    switch (prefs.dateFormat) {
      case "iso": {
        const dateFormatter = new Intl.DateTimeFormat("en-CA", {
          timeZone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
        })
        return dateFormatter.format(date)
      }

      case "us":
        return date.toLocaleDateString("en-US", {
          timeZone,
          month: "2-digit",
          day: "2-digit",
          year: "numeric",
        })

      case "eu":
        return date.toLocaleDateString("en-GB", {
          timeZone,
          day: "2-digit",
          month: "2-digit",
          year: "numeric",
        })

      default: {
        const dateFormatter = new Intl.DateTimeFormat("en-CA", {
          timeZone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
        })
        return dateFormatter.format(date)
      }
    }
  } catch (error) {
    console.error("Error formatting date:", error)
    return new Date(timestamp * 1000).toLocaleDateString()
  }
}

/**
 * Format time only (without date) using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted time string
 */
export function formatTimeOnly(timestamp: number, preferences?: DateTimePreferences): string {
  if (!timestamp || timestamp === 0) return "N/A"

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  try {
    return date.toLocaleTimeString([], {
      timeZone: prefs.timezone,
      hour: "2-digit",
      minute: "2-digit",
      hour12: prefs.timeFormat === "12h",
    })
  } catch (error) {
    console.error("Error formatting time:", error)
    return new Date(timestamp * 1000).toLocaleTimeString()
  }
}

/**
 * Format a JavaScript Date object using user preferences
 * @param date JavaScript Date object
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date/time string
 */
export function formatDate(date: Date, preferences?: DateTimePreferences): string {
  const timestamp = Math.floor(date.getTime() / 1000)
  return formatTimestamp(timestamp, preferences)
}

/**
 * Format the "Added On" date for torrent table columns using user preferences
 * This maintains compatibility with the existing TorrentTableColumns component
 * @param addedOn Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date/time string
 */
export function formatAddedOn(addedOn: number, preferences?: DateTimePreferences): string {
  return formatTimestamp(addedOn, preferences)
}

/**
 * Format an ISO 8601 timestamp string using user preferences
 * Useful for activity logs and event timestamps from APIs
 * @param isoTimestamp ISO 8601 timestamp string (e.g., "2025-01-15T10:30:00Z")
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date/time string or the original string if parsing fails
 */
export function formatISOTimestamp(isoTimestamp: string, preferences?: DateTimePreferences): string {
  if (!isoTimestamp) return "N/A"

  try {
    const date = new Date(isoTimestamp)
    if (isNaN(date.getTime())) return isoTimestamp

    const timestamp = Math.floor(date.getTime() / 1000)
    return formatTimestamp(timestamp, preferences)
  } catch {
    return isoTimestamp
  }
}

/**
 * Format relative time from a date (e.g., "5 minutes ago" or "3 minutes")
 * Always returns relative time, independent of user preferences.
 * Use this for status displays where relative time is always appropriate.
 * @param date Date to format
 * @param addSuffix Whether to add "ago" suffix (default: true)
 * @returns Relative time string
 */
export function formatRelativeTime(date: Date, addSuffix = true): string {
  return formatLocalizedRelativeTime(date, { withDirection: addSuffix })
}

export function formatSearchDuration(durationMs: number, secondsPrecision: number): string {
  if (durationMs < 1000) {
    return `${durationMs}ms`
  }
  return `${(durationMs / 1000).toFixed(secondsPrecision)}s`
}

/**
 * Format time as HH:mm:ss
 * @param date Date to format
 * @returns Time string in HH:mm:ss format
 */
export function formatTimeHMS(date: Date): string {
  const hours = date.getHours().toString().padStart(2, "0")
  const minutes = date.getMinutes().toString().padStart(2, "0")
  const seconds = date.getSeconds().toString().padStart(2, "0")
  return `${hours}:${minutes}:${seconds}`
}
