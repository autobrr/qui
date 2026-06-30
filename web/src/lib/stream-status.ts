// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

export type StreamFallbackVariant = "degraded" | "offline"

export interface StreamFallbackPlan {
  /** Banner tone: amber when data is stale-but-usable, red on a cold start. */
  tone: "warning" | "error"
  variant: StreamFallbackVariant
  /** Whether to render a "data from N ago" age line. */
  showStaleAge: boolean
}

/**
 * resolveStreamFallbackStatus decides how the polling-fallback banner should present
 * when the live stream drops.
 *
 * When rows are already on screen the data is stale but still usable, so we degrade
 * to a calm amber "showing cached data" state and surface how old it is. Only a cold
 * start with no rows at all is a true red error. This keeps a slow or saturated
 * qBittorrent from blanking the table (issue #2052).
 */
export function resolveStreamFallbackStatus(args: {
  hasData: boolean
  hasLastSuccessfulSync: boolean
}): StreamFallbackPlan {
  if (!args.hasData) {
    return { tone: "error", variant: "offline", showStaleAge: false }
  }

  return { tone: "warning", variant: "degraded", showStaleAge: args.hasLastSuccessfulSync }
}
