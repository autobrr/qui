/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"
import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"

type RelativeUnit = "second" | "minute" | "hour" | "day" | "week" | "month" | "year"

function translateCommon(key: string, options?: Record<string, unknown>): string {
  return i18n.t(key, { ns: "common", ...options })
}

function getCurrentLanguage(): string {
  return i18n.resolvedLanguage || i18n.language || "en"
}

function getDisplayLocales(): string[] {
  const language = getCurrentLanguage()
  const baseLanguage = language.split("-")[0]
  return Array.from(new Set([language, baseLanguage, "en"]))
}

function getDateLocales(style: "iso" | "us" | "eu"): string[] {
  const baseLanguage = getCurrentLanguage().split("-")[0]

  switch (style) {
    case "iso":
      return Array.from(new Set([`${baseLanguage}-CA`, ...getDisplayLocales(), "en-CA"]))
    case "us":
      return Array.from(new Set([`${baseLanguage}-US`, ...getDisplayLocales(), "en-US"]))
    case "eu":
      return Array.from(new Set([`${baseLanguage}-GB`, ...getDisplayLocales(), "en-GB"]))
  }
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

  return {
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    timeFormat: "24h",
    dateFormat: "iso",
  }
}

function getNotAvailable(): string {
  return translateCommon("dateTime.na")
}

function getRelativeUnitLabel(value: number, unit: RelativeUnit): string {
  return translateCommon(`dateTime.units.${unit}`, { count: value })
}

function formatRelativeUnit(value: number, unit: RelativeUnit, addSuffix: boolean, isFuture: boolean): string {
  if (value <= 0) {
    return translateCommon("dateTime.justNow")
  }

  const label = getRelativeUnitLabel(value, unit)
  if (!addSuffix) {
    return label
  }

  return translateCommon(isFuture ? "dateTime.future" : "dateTime.past", { value: label })
}

function getRelativeParts(absDiffMs: number): { value: number; unit: RelativeUnit } {
  const diffSec = Math.floor(absDiffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)
  const diffWeek = Math.floor(diffDay / 7)
  const diffMonth = Math.floor(diffDay / 30)
  const diffYear = Math.floor(diffDay / 365)

  if (diffSec < 60) return { value: 0, unit: "second" }
  if (diffMin < 60) return { value: diffMin, unit: "minute" }
  if (diffHour < 24) return { value: diffHour, unit: "hour" }
  if (diffDay < 7) return { value: diffDay, unit: "day" }
  if (diffWeek < 4) return { value: diffWeek, unit: "week" }
  if (diffMonth < 12 && diffMonth > 0) return { value: diffMonth, unit: "month" }
  if (diffYear > 0) return { value: diffYear, unit: "year" }
  if (diffWeek > 0) return { value: diffWeek, unit: "week" }
  if (diffDay > 0) return { value: diffDay, unit: "day" }
  return { value: 0, unit: "second" }
}

function formatRelativeToNow(date: Date, addSuffix = true): string {
  const diffMs = date.getTime() - Date.now()
  const isFuture = diffMs > 0
  const { value, unit } = getRelativeParts(Math.abs(diffMs))

  if (value === 0) {
    return translateCommon("dateTime.justNow")
  }

  return formatRelativeUnit(value, unit, addSuffix, isFuture)
}

function formatDateAndTime(date: Date, preferences: DateTimePreferences, includeSeconds = false): string {
  const timeZone = preferences.timezone
  const hour12 = preferences.timeFormat === "12h"
  const secondOption = includeSeconds ? { second: "2-digit" as const } : {}

  switch (preferences.dateFormat) {
    case "iso": {
      const dateFormatter = new Intl.DateTimeFormat(getDateLocales("iso"), {
        timeZone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      })
      const timeFormatter = new Intl.DateTimeFormat(getDisplayLocales(), {
        timeZone,
        hour: "2-digit",
        minute: "2-digit",
        ...secondOption,
        hour12,
      })
      return `${dateFormatter.format(date)} ${timeFormatter.format(date)}`
    }
    case "us":
      return date.toLocaleString(getDateLocales("us"), {
        timeZone,
        month: "2-digit",
        day: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        ...secondOption,
        hour12,
      })
    case "eu":
      return date.toLocaleString(getDateLocales("eu"), {
        timeZone,
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        ...secondOption,
        hour12,
      })
    default:
      return date.toLocaleString(getDisplayLocales(), {
        timeZone,
        hour: "2-digit",
        minute: "2-digit",
        ...secondOption,
        hour12,
      })
  }
}

function formatDateOnlyAbsolute(date: Date, preferences: DateTimePreferences): string {
  const timeZone = preferences.timezone

  switch (preferences.dateFormat) {
    case "iso":
      return new Intl.DateTimeFormat(getDateLocales("iso"), {
        timeZone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(date)
    case "us":
      return date.toLocaleDateString(getDateLocales("us"), {
        timeZone,
        month: "2-digit",
        day: "2-digit",
        year: "numeric",
      })
    case "eu":
      return date.toLocaleDateString(getDateLocales("eu"), {
        timeZone,
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
      })
    default:
      return date.toLocaleDateString(getDisplayLocales(), { timeZone })
  }
}

/**
 * Format a timestamp using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @param includeSeconds Whether to include seconds in absolute timestamps
 * @returns Formatted date/time string
 */
export function formatTimestamp(timestamp: number, preferences?: DateTimePreferences, includeSeconds = false): string {
  if (!timestamp || timestamp === 0) return getNotAvailable()

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  if (prefs.dateFormat === "relative") {
    return formatRelativeToNow(date)
  }

  try {
    return formatDateAndTime(date, prefs, includeSeconds)
  } catch (error) {
    console.error("Error formatting timestamp:", error)
    return new Date(timestamp * 1000).toLocaleString(getDisplayLocales())
  }
}

/**
 * Format a date only (without time) using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @returns Formatted date string
 */
export function formatDateOnly(timestamp: number, preferences?: DateTimePreferences): string {
  if (!timestamp || timestamp === 0) return getNotAvailable()

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  if (prefs.dateFormat === "relative") {
    const diffMs = Date.now() - date.getTime()
    const diffDay = Math.floor(diffMs / (1000 * 60 * 60 * 24))

    if (diffDay === 0 && diffMs >= 0) return translateCommon("dateTime.today")
    if (diffDay === 1) return translateCommon("dateTime.yesterday")
    if (diffDay > 1 && diffDay < 7) return formatRelativeUnit(diffDay, "day", true, false)

    return formatRelativeToNow(date)
  }

  try {
    return formatDateOnlyAbsolute(date, prefs)
  } catch (error) {
    console.error("Error formatting date:", error)
    return new Date(timestamp * 1000).toLocaleDateString(getDisplayLocales())
  }
}

/**
 * Format time only (without date) using user preferences
 * @param timestamp Unix timestamp in seconds
 * @param preferences Optional preferences (will use stored if not provided)
 * @param includeSeconds Whether to include seconds in the formatted time
 * @returns Formatted time string
 */
export function formatTimeOnly(timestamp: number, preferences?: DateTimePreferences, includeSeconds = false): string {
  if (!timestamp || timestamp === 0) return getNotAvailable()

  const prefs = preferences || getStoredPreferences()
  const date = new Date(timestamp * 1000)

  try {
    return date.toLocaleTimeString(getDisplayLocales(), {
      timeZone: prefs.timezone,
      hour: "2-digit",
      minute: "2-digit",
      ...(includeSeconds ? { second: "2-digit" } : {}),
      hour12: prefs.timeFormat === "12h",
    })
  } catch (error) {
    console.error("Error formatting time:", error)
    return new Date(timestamp * 1000).toLocaleTimeString(getDisplayLocales())
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
  if (!isoTimestamp) return getNotAvailable()

  try {
    const date = new Date(isoTimestamp)
    if (Number.isNaN(date.getTime())) return isoTimestamp

    const timestamp = Math.floor(date.getTime() / 1000)
    return formatTimestamp(timestamp, preferences)
  } catch {
    return isoTimestamp
  }
}

/**
 * Format relative time from a date-like value.
 * Always returns relative time, independent of user preferences.
 * Use this for status displays where relative time is always appropriate.
 * @param value Date, ISO string, or Unix timestamp in seconds
 * @param addSuffix Whether to add suffix text (default: true)
 * @returns Relative time string or "—" for invalid input
 */
export function formatRelativeTime(value?: string | number | Date | null, addSuffix = true): string {
  if (value === undefined || value === null) {
    return "—"
  }

  const date = value instanceof Date ? value : new Date(typeof value === "number" ? value * 1000 : value)
  if (Number.isNaN(date.getTime())) {
    return "—"
  }

  return formatRelativeToNow(date, addSuffix)
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
