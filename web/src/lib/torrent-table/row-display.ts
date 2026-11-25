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

interface Tone {
  className: string
  iconClass: string
  hoverClass: string
  raisedHoverClass: string
}

const RED: Tone = {
  className: "text-destructive border-destructive/40 bg-destructive/10",
  iconClass: "text-destructive",
  hoverClass: "hover:bg-destructive/10",
  raisedHoverClass: "hover:border-destructive hover:bg-destructive/20",
}

const PURPLE: Tone = {
  className: "text-purple-500 border-purple-500/40 bg-purple-500/10",
  iconClass: "text-purple-500",
  hoverClass: "hover:bg-purple-500/10",
  raisedHoverClass: "hover:border-purple-500 hover:bg-purple-500/20",
}

const YELLOW: Tone = {
  className: "text-yellow-500 border-yellow-500/40 bg-yellow-500/10",
  iconClass: "text-yellow-500",
  hoverClass: "hover:bg-yellow-500/10",
  raisedHoverClass: "hover:border-yellow-500 hover:bg-yellow-500/20",
}

export const TRACKER_HEALTH_DISPLAY: Record<TrackerHealth, Tone & { labelKey: string, icon: LucideIcon }> = {
  unregistered: { labelKey: "tableColumns.unregistered", icon: XCircle, ...RED },
  tracker_down: { labelKey: "tableColumns.trackerDown", icon: AlertCircle, ...YELLOW },
  tracker_error: { labelKey: "tableColumns.trackerError", icon: XCircle, ...PURPLE },
}

export type CopyProblem = "missingFiles" | "error" | TrackerHealth

export const COPY_PROBLEM_DISPLAY: Record<CopyProblem, Tone & { labelKey: string }> = {
  missingFiles: { labelKey: "copyGroups.hiddenProblem.missingFiles", ...RED },
  error: { labelKey: "copyGroups.hiddenProblem.error", ...RED },
  unregistered: { labelKey: "copyGroups.hiddenProblem.unregistered", ...RED },
  tracker_error: { labelKey: "copyGroups.hiddenProblem.trackerError", ...PURPLE },
  tracker_down: { labelKey: "copyGroups.hiddenProblem.trackerDown", ...YELLOW },
}

export function getRowBackgroundClass(isRowSelected: boolean, isSelected: boolean, rowIndex: number): string {
  if (isRowSelected || isSelected) return "bg-accent"
  if (rowIndex % 2 === 1) return "bg-muted/40"
  return ""
}

export const SELECT_COLUMN_WIDTH = 40
export const GROUPED_SELECT_COLUMN_WIDTH = 112

export function centersSelectCheckbox(columnWidth: number): boolean {
  return columnWidth <= SELECT_COLUMN_WIDTH
}

export function getCopyGroupAccentClass(isTopRow: boolean): string {
  return `before:pointer-events-none before:absolute before:left-0 before:bottom-0 before:w-0.5 before:bg-primary before:content-[''] ${isTopRow ? "before:top-2 before:rounded-t-full" : "before:top-0"}`
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
