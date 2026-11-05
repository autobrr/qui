/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

export function getStateLabel(state: string, t: TFunction): string {
  // Backend input in a key path: "." or ":" resolves a fragment and an inherited name resolves a function.
  if (!/^[a-zA-Z]+$/.test(state) || state in Object.prototype) return t("stateLabelFallback")

  // One t() call: i18next returns the first key it finds, so a miss costs no second pass through the post-processors.
  // The fallback sits outside stateLabels because "unknown" is itself a qBittorrent state.
  return t([`stateLabels.${state}`, "stateLabelFallback"])
}

// formatReannounceIn gives the time until qBittorrent next announces the
// torrent. qBittorrent sends 0 for a stopped torrent, so a stopped torrent
// shows a dash, not "now".
export function formatReannounceIn(state: string, seconds: number | undefined, t: TFunction): string {
  if (seconds === undefined || state === "stoppedDL" || state === "stoppedUP") return "—"

  // Negative values mean "never" or "not applicable"
  if (seconds < 0) return "-"

  // Zero means "now" (just announced or about to announce)
  if (seconds === 0) return t("tableColumns.now")

  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)

  if (minutes < 1) {
    return "< 1m"
  }

  if (hours < 1) {
    return `${minutes}m`
  }

  const remainingMinutes = minutes % 60
  if (remainingMinutes === 0) {
    return `${hours}h`
  }

  return `${hours}h ${remainingMinutes}m`
}
