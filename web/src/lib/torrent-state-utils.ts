/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Human-friendly labels for qBittorrent torrent states
const TORRENT_STATE_LABELS: Record<string, string> = {
  // Downloading related
  downloading: "Downloading",
  metaDL: "Fetching Metadata",
  allocating: "Allocating",
  stalledDL: "Stalled",
  queuedDL: "Queued",
  checkingDL: "Checking",
  forcedDL: "(F) Downloading",

  // Uploading / Seeding related
  uploading: "Seeding",
  stalledUP: "Seeding",
  queuedUP: "Queued",
  checkingUP: "Checking",
  forcedUP: "(F) Seeding",

  // Paused / Stopped
  pausedDL: "Paused",
  pausedUP: "Completed",
  stoppedDL: "Stopped",
  stoppedUP: "Completed",

  // Other
  error: "Error",
  missingFiles: "Missing Files",
  checkingResumeData: "Checking Resume Data",
  moving: "Moving",
}

export function getStateLabel(state: string): string {
  return TORRENT_STATE_LABELS[state] ?? state
}

export type StatusBadgeVariant = "default" | "secondary" | "destructive" | "outline"

export interface StatusBadgeMeta {
  label: string
  variant: StatusBadgeVariant
  className: string
  iconClass: string
}

/**
 * Returns status badge styling based on torrent state and tracker health.
 * Centralizes all status color logic for consistency across views.
 */
export function getStatusBadgeMeta(
  state: string,
  trackerHealth?: string | null,
  supportsTrackerHealth: boolean = true
): StatusBadgeMeta {
  let label = getStateLabel(state)
  let variant: StatusBadgeVariant = "outline"
  let className = ""
  let iconClass = "text-muted-foreground"

  // Tracker health takes priority
  if (supportsTrackerHealth && trackerHealth) {
    if (trackerHealth === "tracker_down") {
      return {
        label: "Tracker Down",
        variant: "outline",
        className: "text-yellow-500 border-yellow-500/40 bg-yellow-500/10",
        iconClass: "text-yellow-500",
      }
    }
    if (trackerHealth === "unregistered") {
      return {
        label: "Unregistered",
        variant: "outline",
        className: "text-destructive border-destructive/40 bg-destructive/10",
        iconClass: "text-destructive",
      }
    }
  }

  // Apply semantic status colors based on state
  switch (state) {
    // Downloading states - green
    case "downloading":
    case "metaDL":
    case "forcedDL":
    case "allocating":
    case "checkingDL":
      variant = "default"
      className = "text-green-400 border-green-400/40 bg-green-400/10"
      iconClass = "text-green-400"
      break
    // Uploading/seeding states - blue
    case "uploading":
    case "forcedUP":
      variant = "default"
      className = "text-blue-400 border-blue-400/40 bg-blue-400/10"
      iconClass = "text-blue-400"
      break
    // Stalled states
    case "stalledDL":
    case "stalledUP":
      variant = "secondary"
      iconClass = "text-secondary-foreground"
      break
    // Error states
    case "error":
    case "missingFiles":
      variant = "destructive"
      iconClass = "text-destructive"
      break
    // Queued/paused states
    case "pausedDL":
    case "pausedUP":
    case "stoppedDL":
    case "stoppedUP":
    case "queuedDL":
    case "queuedUP":
      variant = "secondary"
      iconClass = "text-secondary-foreground"
      break
    // Other states (checking, moving, etc.)
    default:
      variant = "outline"
      iconClass = "text-muted-foreground"
      break
  }

  return { label, variant, className, iconClass }
}

