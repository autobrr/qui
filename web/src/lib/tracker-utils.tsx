/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"

/**
 * Check if a tracker URL is a valid HTTP/HTTPS URL.
 * Returns false for non-URL entries like DHT, PeX, LSD.
 */
export function isValidTrackerUrl(url: string): boolean {
  try {
    new URL(url)
    return true
  } catch {
    return false
  }
}

/**
 * Get a status badge for a tracker based on its status code.
 * @param status - The tracker status code (0-4)
 * @param compact - Whether to use compact styling (for tables)
 */
export function getTrackerStatusBadge(status: number, compact = false) {
  const compactClass = compact ? "text-[10px] px-1.5 py-0" : ""
  const workingClass = compact ? `${compactClass} bg-green-500` : ""

  switch (status) {
    case 0:
      return <Badge variant="secondary" className={compactClass}>Disabled</Badge>
    case 1:
      return <Badge variant="secondary" className={compactClass}>Not contacted</Badge>
    case 2:
      return <Badge variant="default" className={workingClass}>Working</Badge>
    case 3:
      return <Badge variant="default" className={compactClass}>Updating</Badge>
    case 4:
      return <Badge variant="destructive" className={compactClass}>Error</Badge>
    default:
      return <Badge variant="outline" className={compactClass}>Unknown</Badge>
  }
}
