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
  /** For a surface that already has the tinted background, such as the Copy group pill. */
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

// One entry per tracker health. Every view that shows tracker health reads its colors and icon here.
export const TRACKER_HEALTH_DISPLAY: Record<TrackerHealth, Tone & { labelKey: string, icon: LucideIcon }> = {
  unregistered: { labelKey: "tableColumns.unregistered", icon: XCircle, ...RED },
  tracker_down: { labelKey: "tableColumns.trackerDown", icon: AlertCircle, ...YELLOW },
  tracker_error: { labelKey: "tableColumns.trackerError", icon: XCircle, ...PURPLE },
}

export type CopyProblem = "missingFiles" | "error" | TrackerHealth

// What the pill of a closed Copy group shows for the worst problem of its hidden copies.
export const COPY_PROBLEM_DISPLAY: Record<CopyProblem, Tone & { labelKey: string }> = {
  missingFiles: { labelKey: "copyGroups.hiddenProblem.missingFiles", ...RED },
  error: { labelKey: "copyGroups.hiddenProblem.error", ...RED },
  unregistered: { labelKey: "copyGroups.hiddenProblem.unregistered", ...RED },
  tracker_error: { labelKey: "copyGroups.hiddenProblem.trackerError", ...PURPLE },
  tracker_down: { labelKey: "copyGroups.hiddenProblem.trackerDown", ...YELLOW },
}

// Returns the background class for a row based on selection state and zebra striping
export function getRowBackgroundClass(isRowSelected: boolean, isSelected: boolean, rowIndex: number): string {
  if (isRowSelected || isSelected) return "bg-accent"
  if (rowIndex % 2 === 1) return "bg-muted/40"
  return ""
}

export const SELECT_COLUMN_WIDTH = 40
// With the Name column hidden, the select column of a grouped list also holds
// the Copy group pill, so the pill never takes room from a data value.
export const GROUPED_SELECT_COLUMN_WIDTH = 112

// A widened select column also holds the Copy group pill, so its checkbox
// aligns left in the header and in every row instead of in the center.
export function centersSelectCheckbox(columnWidth: number): boolean {
  return columnWidth <= SELECT_COLUMN_WIDTH
}

// The left bar on every row of an open Copy group. On the top row the bar
// starts 8px down, so two open groups next to each other stay apart.
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
