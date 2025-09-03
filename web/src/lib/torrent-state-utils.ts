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
  stalledUP: "Stalled",
  queuedUP: "Queued",
  checkingUP: "Checking",
  forcedUP: "(F) Seeding",

  // Paused / Stopped
  pausedDL: "Paused",
  pausedUP: "Paused",
  stoppedDL: "Stopped",
  stoppedUP: "Stopped",

  // Other
  error: "Error",
  missingFiles: "Missing Files",
  checkingResumeData: "Checking Resume Data",
  moving: "Moving",
}

export function getStateLabel(state: string): string {
  return TORRENT_STATE_LABELS[state] ?? state
}



