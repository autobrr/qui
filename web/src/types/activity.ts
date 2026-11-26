/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type ActivityEventKind =
  | "backup.run"
  | "dirscan.run"
  | "orphanscan.run"
  | "discscan.run"
  | "crossseed.status"
  | "crossseed.search"
  | "reannounce.activity"
  | "automation.activity"
  | "indexer.activity"
  | "search.history"
  | "tracker.icons"
  | "theme.settings"
  | "theme.custom"
  | "client.settings"

export interface ActivityEvent {
  kind: ActivityEventKind
  instanceId?: number
  resourceId?: string
  timestamp: string
}

export interface ActivityStreamPayload {
  type: "activity"
  activity?: ActivityEvent
}
