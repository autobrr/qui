/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"

// Get stored preferences from localStorage
function getStoredPreferences(): DateTimePreferences {
  try {
    const stored = localStorage.getItem("qui-datetime-preferences")
    if (stored) {
      const parsed = JSON.parse(stored)
      return {
        timezone: parsed.timezone || "UTC",
        timeFormat: parsed.timeFormat || "24h",
        dateFormat: parsed.dateFormat || "iso"
      }
    }
  } catch (error) {
    console.error("Failed to load date/time preferences:", error)
  }

  // Fallback to defaults
  return {
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    timeFormat: "24h",
    dateFormat: "iso"
  }
}

// Calculate relative time display
function getRelativeTime(date: Date): string {
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)
  const diffWeek = Math.floor(diffDay / 7)
  const diffMonth = Math.floor(diffDay / 30)
  const diffYear = Math.floor(diffDay / 365)

  if (diffSec < 60) return "Just now"
  if (diffMin < 60) return `${diffMin} minute${diffMin !== 1 ? "s" : ""} ago`
  if (diffHour < 24) return `${diffHour} hour${diffHour !== 1 ? "s" : ""} ago`
  if (diffDay < 7) return `${diffDay} day${diffDay !== 1 ? "s" : ""} ago`
  if (diffWeek < 4) return `${diffWeek} week${diffWeek !== 1 ? "s" : ""} ago`
  if (diffMonth < 12 && diffMonth > 0) return `${diffMonth} month${diffMonth !== 1 ? "s" : ""} ago`
  if (diffYear > 0) return `${diffYear} year${diffYear !== 1 ? "s" : ""} ago`
  
  // Fallback for edge cases (like exactly 0 months but some weeks)
  if (diffWeek > 0) return `${diffWeek} week${diffWeek !== 1 ? "s" : ""} ago`
  if (diffDay > 0) return `${diffDay} day${diffDay !== 1 ? "s" : ""} ago`
  return "Just now"
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
    return getRelativeTime(date)
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
          hour12
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
          hour12
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
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffDay = Math.floor(diffMs / (1000 * 60 * 60 * 24))
    
    if (diffDay === 0) return "Today"
    if (diffDay === 1) return "Yesterday"
    if (diffDay < 7) return `${diffDay} days ago`
    
    return getRelativeTime(date)
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
          year: "numeric"
        })
      
      case "eu":
        return date.toLocaleDateString("en-GB", {
          timeZone,
          day: "2-digit",
          month: "2-digit",
          year: "numeric"
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
      hour12: prefs.timeFormat === "12h"
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
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const isFuture = diffMs < 0
  const absDiffMs = Math.abs(diffMs)

  const diffSec = Math.floor(absDiffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)
  const diffWeek = Math.floor(diffDay / 7)
  const diffMonth = Math.floor(diffDay / 30)
  const diffYear = Math.floor(diffDay / 365)

  let value: string
  if (diffSec < 60) value = "just now"
  else if (diffMin < 60) value = `${diffMin} minute${diffMin !== 1 ? "s" : ""}`
  else if (diffHour < 24) value = `${diffHour} hour${diffHour !== 1 ? "s" : ""}`
  else if (diffDay < 7) value = `${diffDay} day${diffDay !== 1 ? "s" : ""}`
  else if (diffWeek < 4) value = `${diffWeek} week${diffWeek !== 1 ? "s" : ""}`
  else if (diffMonth < 12 && diffMonth > 0) value = `${diffMonth} month${diffMonth !== 1 ? "s" : ""}`
  else if (diffYear > 0) value = `${diffYear} year${diffYear !== 1 ? "s" : ""}`
  else if (diffWeek > 0) value = `${diffWeek} week${diffWeek !== 1 ? "s" : ""}`
  else if (diffDay > 0) value = `${diffDay} day${diffDay !== 1 ? "s" : ""}`
  else value = "just now"

  if (!addSuffix || value === "just now") return value
  return isFuture ? `in ${value}` : `${value} ago`
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