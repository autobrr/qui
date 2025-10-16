/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const k = 1024
  const sizes = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

const SECOND_FACTOR = 1000

function pad2(value: number): string {
  return String(value).padStart(2, "0")
}

function toDate(timestamp: number): Date | null {
  if (!timestamp || timestamp === 0) return null
  return new Date(timestamp * SECOND_FACTOR)
}

function formatDateFromDate(date: Date): string {
  const year = String(date.getFullYear())
  const month = pad2(date.getMonth() + 1)
  const day = pad2(date.getDate())
  return `${year}/${month}/${day}`
}

function formatTimeFromDate(date: Date, includeSeconds: boolean): string {
  const hours = pad2(date.getHours())
  const minutes = pad2(date.getMinutes())
  const seconds = pad2(date.getSeconds())
  return includeSeconds ? `${hours}:${minutes}:${seconds}` : `${hours}:${minutes}`
}

export function formatTimestamp(timestamp: number): string {
  const date = toDate(timestamp)
  if (!date) return "N/A"
  return formatDateTime(timestamp)
}

/**
 * Format a Unix timestamp to YY/MM/dd string (24-hour base format)
 * @param timestamp - Unix timestamp in seconds
 */
export function formatDate(timestamp: number): string {
  const date = toDate(timestamp)
  if (!date) return "N/A"

  return formatDateFromDate(date)
}

/**
 * Format a Unix timestamp to YY/MM/dd HH:mm[:ss] in 24-hour time.
 * @param timestamp - Unix timestamp in seconds
 * @param includeSeconds - Whether to include seconds (default: true)
 */
export function formatDateTime(timestamp: number, includeSeconds = true): string {
  const date = toDate(timestamp)
  if (!date) return "N/A"

  return `${formatDateFromDate(date)} ${formatTimeFromDate(date, includeSeconds)}`
}

/**
 * Format a Unix timestamp to HH:mm[:ss] in 24-hour time.
 * @param timestamp - Unix timestamp in seconds
 * @param includeSeconds - Whether to include seconds (default: false)
 */
export function formatTime(timestamp: number, includeSeconds = false): string {
  const date = toDate(timestamp)
  if (!date) return "N/A"

  return formatTimeFromDate(date, includeSeconds)
}

/**
 * Format a Unix timestamp to an ISO 8601 date string (YYYY-MM-DD)
 * Useful for APIs, sorting, and unambiguous date representation
 * @param timestamp - Unix timestamp in seconds
 * @returns ISO formatted date string (e.g., "2024-12-31")
 */
export function formatISODate(timestamp: number): string {
  if (!timestamp || timestamp === 0) return "N/A"

  const date = new Date(timestamp * 1000)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, "0")
  const day = String(date.getDate()).padStart(2, "0")

  return `${year}-${month}-${day}`
}

/**
 * Get the appropriate color for a torrent ratio based on predefined thresholds
 * @param ratio - The ratio value (uploaded/downloaded)
 * @returns CSS custom property string for the appropriate color
 */
export function getRatioColor(ratio: number): string {
  if (ratio < 0) return ""

  if (ratio < 0.5) {
    return "var(--chart-5)" // very bad - lowest/darkest
  } else if (ratio < 1.0) {
    return "var(--chart-4)" // bad - below 1.0
  } else if (ratio < 2.0) {
    return "var(--chart-3)" // okay - above 1.0
  } else if (ratio < 5.0) {
    return "var(--chart-2)" // good - healthy ratio
  } else {
    return "var(--chart-1)" // excellent - best ratio
  }
}

export function formatDuration(seconds: number): string {
  if (seconds === 0) return "0s"
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60

  const parts = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0) parts.push(`${minutes}m`)
  if (secs > 0) parts.push(`${secs}s`)

  return parts.join(" ")
}

export function formatErrorMessage(error: string | undefined): string {
  if (!error) return "Unknown error"

  const normalized = error.trim()
  if (!normalized) return "Unknown error"

  const cleaned = normalized.replace(/^(failed to create client: |failed to connect to qBittorrent instance: |connection failed: |error: )/i, "")
  if (!cleaned) return "Unknown error"

  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}

export async function copyTextToClipboard(text: string): Promise<void> {
  const hasClipboardApi = typeof navigator !== "undefined" && "clipboard" in navigator
  const canUseAsyncApi = hasClipboardApi && typeof window !== "undefined" && window.isSecureContext

  if (canUseAsyncApi) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch (err) {
      console.error("Copy to clipboard unsuccessful, falling back to execCommand: ", err)
      // Fall through to synchronous fallback below.
    }
  }

  // Fallback for:
  // - Browsers without Clipboard API support
  // - Non-secure contexts (HTTP sites, not localhost)
  // - Cases where the async Clipboard API rejects (e.g., permission denied)
  copyTextToClipboardFallback(text)
}

function copyTextToClipboardFallback(text: string): void {
  const textarea = document.createElement("textarea")
  textarea.value = text
  textarea.setAttribute("readonly", "")
  textarea.style.position = "absolute"
  textarea.style.opacity = "0"
  textarea.style.top = "0"
  textarea.style.left = "0"
  document.body.appendChild(textarea)
  const selection = document.getSelection()
  const originalRange = selection?.rangeCount ? selection.getRangeAt(0).cloneRange() : null
  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, text.length)
  let listenerTriggered = false
  const listener = (event: ClipboardEvent) => {
    if (event.clipboardData) {
      event.clipboardData.setData("text/plain", text)
      listenerTriggered = true
      event.preventDefault()
    }
  }
  document.addEventListener("copy", listener, true)
  try {
    const successful = document.execCommand("copy")
    if (!successful && !listenerTriggered) {
      throw new Error("Failed to copy text using fallback method")
    }
    console.log("Text copied to clipboard successfully using fallback.")
  } catch (err) {
    console.error("Failed to copy text using fallback method: ", err)
    throw err
  } finally {
    document.removeEventListener("copy", listener, true)
    document.body.removeChild(textarea)
    if (originalRange && selection) {
      selection.removeAllRanges()
      selection.addRange(originalRange)
    }
  }
}
