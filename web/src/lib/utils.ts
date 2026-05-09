/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Automation } from "@/types"
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

export function formatDurationCompact(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
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

/**
 * Simplify verbose error messages by extracting the root cause.
 * Useful for displaying cleaner error messages in UIs.
 * @param reason - The full error message/reason string
 * @returns Simplified error message with root cause
 */
export function formatErrorReason(reason: string): string {
  if (!reason) return reason

  const rootCauses = [
    "context deadline exceeded",
    "connection refused",
    "no such host",
    "connection reset",
    "timeout",
  ]

  for (const cause of rootCauses) {
    if (reason.toLowerCase().includes(cause)) {
      const firstColon = reason.indexOf(":")
      const action = firstColon > 0 ? reason.substring(0, firstColon).trim() : "operation failed"
      return `${action} (${cause})`
    }
  }

  if (reason.length > 150) {
    return reason.substring(0, 147) + "..."
  }

  return reason
}

/**
 * Parse tracker domains from an Automation.
 * Returns trackerDomains array if present, otherwise parses trackerPattern.
 * @param rule - The automation to parse domains from
 * @returns Array of tracker domain strings
 */
export function parseTrackerDomains(rule: Automation): string[] {
  if (rule.trackerDomains && rule.trackerDomains.length > 0) {
    return rule.trackerDomains
  }
  if (!rule.trackerPattern) return []
  return rule.trackerPattern
    .split(/[|,;]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

/**
 * Normalize tracker domain values to a flat list of domains.
 * Accepts values that may already be individual domains or comma-separated groups (e.g. "a.com,b.net").
 */
export function normalizeTrackerDomains(values: string[]): string[] {
  const result: string[] = []
  const seen = new Set<string>()

  for (const raw of values) {
    for (const part of raw.split(",")) {
      const normalized = part.trim().toLowerCase()
      if (!normalized) continue
      if (seen.has(normalized)) continue
      seen.add(normalized)
      result.push(normalized)
    }
  }

  return result
}
