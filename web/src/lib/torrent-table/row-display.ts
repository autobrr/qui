/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getStateLabel } from "@/lib/torrent-state-utils"
import type { Torrent } from "@/types"
import type { TFunction } from "i18next"

// Returns the background class for a row based on selection state and zebra striping
export function getRowBackgroundClass(isRowSelected: boolean, isSelected: boolean, rowIndex: number): string {
  if (isRowSelected || isSelected) return "bg-accent"
  if (rowIndex % 2 === 1) return "bg-muted/40"
  return ""
}

export function getStatusBadgeVariant(state: string): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case "downloading":
      return "default"
    case "stalledDL":
      return "secondary"
    case "uploading":
      return "default"
    case "stalledUP":
      return "secondary"
    case "pausedDL":
    case "pausedUP":
      return "secondary"
    case "error":
    case "missingFiles":
      return "destructive"
    default:
      return "outline"
  }
}

export function getStatusBadgeProps(torrent: Torrent, supportsTrackerHealth: boolean, t: TFunction): {
  variant: "default" | "secondary" | "destructive" | "outline"
  label: string
  className: string
} {
  const baseVariant = getStatusBadgeVariant(torrent.state)
  let variant = baseVariant
  let label = getStateLabel(torrent.state, t)
  let className = ""

  if (supportsTrackerHealth) {
    const trackerHealth = torrent.tracker_health ?? null
    if (trackerHealth === "tracker_down") {
      label = t("tableColumns.trackerDown")
      variant = "outline"
      className = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10"
    } else if (trackerHealth === "tracker_error") {
      label = t("tableColumns.trackerError")
      variant = "outline"
      className = "text-orange-500 border-orange-500/40 bg-orange-500/10"
    } else if (trackerHealth === "unregistered") {
      label = t("tableColumns.unregistered")
      variant = "outline"
      className = "text-destructive border-destructive/40 bg-destructive/10"
    }
  }

  return { variant, label, className }
}
