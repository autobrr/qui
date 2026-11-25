/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Automation } from "@/types"
import { getTrackerTokens } from "@/lib/workflow-utils"
import { BYTE_LADDER, formatValueWithUnit, scaleToUnit } from "@/lib/unit-format"
import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function collapseRowClassName(collapsed: boolean) {
  return cn(
    "grid transition-[grid-template-rows,opacity] duration-300 ease-out motion-reduce:transition-none",
    collapsed ? "grid-rows-[0fr] opacity-0" : "grid-rows-[1fr]"
  )
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return formatValueWithUnit(0, "B")
  const scaled = scaleToUnit(bytes, BYTE_LADDER, 1024)
  return formatValueWithUnit(scaled.value, scaled.unit)
}

export function formatBytesOrFallback(bytes: number, fallback: string): string {
  if (bytes < 0) return fallback

  return formatBytes(bytes)
}

export function getRatioColor(ratio: number): string {
  if (ratio < 0) return ""

  if (ratio < 0.5) {
    return "var(--chart-5)"
  } else if (ratio < 1.0) {
    return "var(--chart-4)"
  } else if (ratio < 2.0) {
    return "var(--chart-3)"
  } else if (ratio < 5.0) {
    return "var(--chart-2)"
  } else {
    return "var(--chart-1)"
  }
}

/** @deprecated Prints English units (d, h, m, s). For text that the user reads, use the formatters in `@/lib/dateTimeUtils`. */
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

/** @deprecated Prints English units (s, m, h, d). For text that the user reads, use `formatNarrowAge` in `@/lib/dateTimeUtils`, which takes milliseconds and shows an age under one minute as one minute. */
export function formatDurationCompact(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
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
    }
  }

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

export function parseTrackerDomains(rule: Automation): string[] {
  return getTrackerTokens(rule).map((token) => token.startsWith("!") ? token.slice(1) : token)
}

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

export function joinPath(basePath: string, relativePath: string): string {
  if (!basePath) return relativePath
  const separator = basePath.includes("\\") ? "\\" : "/"
  const cleanBase = basePath.replace(/[\\/]$/, "")
  const normalizedRelative = separator === "\\" ? relativePath.replace(/\//g, "\\") : relativePath
  return `${cleanBase}${separator}${normalizedRelative}`
}

export function breakableName(name: string): string {
  return name.replace(/([._-])/g, "$1\u200B")
}

export function takeTurns<T>(lists: T[][]): T[] {
  const length = Math.max(0, ...lists.map(list => list.length))
  return Array.from({ length }, (_, i) => lists.flatMap(list => (i < list.length ? [list[i]] : []))).flat()
}
