/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// ActivityEventKind mirrors internal/services/activity.Kind. Each kind maps to
// the react-query keys the frontend invalidates when the event arrives.
export type ActivityEventKind =
  | "backup.run"
  | "dirscan.run"
  | "orphanscan.run"
  | "crossseed.status"
  | "crossseed.search"
  | "reannounce.activity"
  | "automation.activity"
  | "indexer.activity"
  | "search.history"
  | "tracker.icons"

// ActivityEvent is a small qui-owned server signal. It carries identifiers only
// (never payload data); the frontend reacts by invalidating the matching query.
export interface ActivityEvent {
  kind: ActivityEventKind
  instanceId?: number
  resourceId?: string
  timestamp: string
}

// ActivityStreamPayload is the envelope for the "activity" SSE event. It is
// disjoint from TorrentStreamPayload and handled by a dedicated listener.
export interface ActivityStreamPayload {
  type: "activity"
  activity?: ActivityEvent
}
