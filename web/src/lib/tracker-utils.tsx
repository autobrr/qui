/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import i18n from "@/i18n"

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
  const tr = (key: string) => String(i18n.t(key as never))
  const compactClass = compact ? "text-[10px] px-1.5 py-0" : ""
  const workingClass = compact ? `${compactClass} bg-green-500` : ""

  switch (status) {
    case 0:
      return <Badge variant="secondary" className={compactClass}>{tr("trackerStatus.disabled")}</Badge>
    case 1:
      return <Badge variant="secondary" className={compactClass}>{tr("trackerStatus.notContacted")}</Badge>
    case 2:
      return <Badge variant="default" className={workingClass}>{tr("trackerStatus.working")}</Badge>
    case 3:
      return <Badge variant="default" className={compactClass}>{tr("trackerStatus.updating")}</Badge>
    case 4:
      return <Badge variant="destructive" className={compactClass}>{tr("trackerStatus.error")}</Badge>
    default:
      return <Badge variant="outline" className={compactClass}>{tr("trackerStatus.unknown")}</Badge>
  }
}
