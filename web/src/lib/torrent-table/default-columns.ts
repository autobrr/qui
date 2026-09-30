/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnOrderState, ColumnVisibilityState } from "@tanstack/react-table"

// The torrent table's default layout: a fresh user's column order and visibility. The order
// also decides where a column missing from a saved order is added (see usePersistedColumnOrder).
const DEFAULT_COLUMNS: ReadonlyArray<{ id: string; visible: boolean }> = [
  { id: "select", visible: true },
  { id: "priority", visible: true },
  { id: "tracker_icon", visible: true },
  { id: "name", visible: true },
  { id: "instance", visible: true },
  { id: "size", visible: true },
  { id: "total_size", visible: false },
  { id: "progress", visible: true },
  { id: "status_icon", visible: true },
  { id: "state", visible: true },
  { id: "num_seeds", visible: true },
  { id: "num_leechs", visible: true },
  { id: "dlspeed", visible: true },
  { id: "upspeed", visible: true },
  { id: "eta", visible: true },
  { id: "ratio", visible: true },
  { id: "popularity", visible: true },
  { id: "category", visible: true },
  { id: "tags", visible: true },
  { id: "added_on", visible: true },
  { id: "completion_on", visible: false },
  { id: "tracker", visible: false },
  { id: "dl_limit", visible: false },
  { id: "up_limit", visible: false },
  { id: "downloaded", visible: false },
  { id: "uploaded", visible: false },
  { id: "downloaded_session", visible: false },
  { id: "uploaded_session", visible: false },
  { id: "amount_left", visible: false },
  { id: "time_active", visible: false },
  { id: "seeding_time", visible: false },
  { id: "save_path", visible: false },
  { id: "completed", visible: false },
  { id: "ratio_limit", visible: false },
  { id: "seen_complete", visible: false },
  { id: "last_activity", visible: false },
  { id: "availability", visible: false },
  { id: "infohash_v1", visible: false },
  { id: "infohash_v2", visible: false },
  { id: "reannounce", visible: false },
  { id: "private", visible: false },
]

// Only the unified view has the instance column.
export const DEFAULT_UNIFIED_COLUMN_ORDER: ColumnOrderState = DEFAULT_COLUMNS.map(col => col.id)
export const DEFAULT_COLUMN_ORDER: ColumnOrderState = DEFAULT_UNIFIED_COLUMN_ORDER.filter(id => id !== "instance")

export const DEFAULT_COLUMN_VISIBILITY: ColumnVisibilityState = Object.fromEntries(
  DEFAULT_COLUMNS.map(col => [col.id, col.visible])
)
