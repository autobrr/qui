/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getStateLabel } from "@/lib/torrent-state-utils"
import type { Torrent } from "@/types"
import type { TFunction } from "i18next"
import { AlertCircle, XCircle, type LucideIcon } from "lucide-react"

// i18n-namespace: torrents

export type TrackerHealth = NonNullable<Torrent["tracker_health"]>

export type StatusBadgeVariant = "default" | "secondary" | "destructive" | "outline"

// One entry per tracker health. Every view that shows tracker health reads its colors and icon here.
export const TRACKER_HEALTH_DISPLAY: Record<TrackerHealth, {
  labelKey: string
  className: string
  iconClass: string
  hoverClass: string
  icon: LucideIcon
}> = {
  unregistered: {
    labelKey: "tableColumns.unregistered",
    className: "text-destructive border-destructive/40 bg-destructive/10",
    iconClass: "text-destructive",
    hoverClass: "hover:bg-destructive/10",
    icon: XCircle,
  },
  tracker_down: {
    labelKey: "tableColumns.trackerDown",
    className: "text-yellow-500 border-yellow-500/40 bg-yellow-500/10",
    iconClass: "text-yellow-500",
    hoverClass: "hover:bg-yellow-500/10",
    icon: AlertCircle,
  },
  tracker_error: {
    labelKey: "tableColumns.trackerError",
    className: "text-orange-500 border-orange-500/40 bg-orange-500/10",
    iconClass: "text-orange-500",
    hoverClass: "hover:bg-orange-500/10",
    icon: XCircle,
  },
}

// Returns the background class for a row based on selection state and zebra striping
export function getRowBackgroundClass(isRowSelected: boolean, isSelected: boolean, rowIndex: number): string {
  if (isRowSelected || isSelected) return "bg-accent"
  if (rowIndex % 2 === 1) return "bg-muted/40"
  return ""
}

export function getStatusBadgeVariant(state: string): StatusBadgeVariant {
  switch (state) {
    case "downloading":
    case "uploading":
      return "default"
    case "stalledDL":
    case "stalledUP":
    case "pausedDL":
    case "pausedUP":
    case "queuedDL":
    case "queuedUP":
      return "secondary"
    case "error":
    case "missingFiles":
      return "destructive"
    default:
      return "outline"
  }
}

const VARIANT_ICON_CLASS: Record<StatusBadgeVariant, string> = {
  default: "text-primary",
  secondary: "text-secondary-foreground",
  destructive: "text-destructive",
  outline: "text-muted-foreground",
}

export function getStatusBadgeProps(
  torrent: Pick<Torrent, "state" | "tracker_health">,
  supportsTrackerHealth: boolean,
  t: TFunction
): {
  variant: StatusBadgeVariant
  label: string
  className: string
  iconClass: string
} {
  const health = supportsTrackerHealth && torrent.tracker_health ? TRACKER_HEALTH_DISPLAY[torrent.tracker_health] : undefined
  if (health) {
    return { variant: "outline", label: t(health.labelKey), className: health.className, iconClass: health.iconClass }
  }

  const variant = getStatusBadgeVariant(torrent.state)
  return { variant, label: getStateLabel(torrent.state, t), className: "", iconClass: VARIANT_ICON_CLASS[variant] }
}
