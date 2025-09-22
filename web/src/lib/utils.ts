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

export function formatTimestamp(timestamp: number): string {
  if (!timestamp || timestamp === 0) return "N/A"
  return new Date(timestamp * 1000).toLocaleString()
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
