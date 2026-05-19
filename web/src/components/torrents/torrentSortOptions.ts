/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getColumnType } from "@/lib/column-filter-utils"

export const TORRENT_SORT_OPTIONS = [
  { value: "added_on", label: "Recently Added" },
  { value: "name", label: "Name" },
  { value: "size", label: "Size" },
  // { value: "total_size", label: "Total Size" },
  { value: "progress", label: "Progress" },
  { value: "state", label: "Status" },
  { value: "priority", label: "Priority" },
  { value: "num_seeds", label: "Seeds" },
  { value: "num_leechs", label: "Leechers" },
  { value: "dlspeed", label: "Download Speed" },
  { value: "upspeed", label: "Upload Speed" },
  { value: "eta", label: "ETA" },
  { value: "ratio", label: "Ratio" },
  { value: "popularity", label: "Popularity" },
  { value: "category", label: "Category" },
  { value: "tags", label: "Tags" },
  { value: "completion_on", label: "Completed On" },
  { value: "tracker", label: "Tracker" },
  { value: "dl_limit", label: "Download Limit" },
  { value: "up_limit", label: "Upload Limit" },
  { value: "downloaded", label: "Downloaded" },
  { value: "uploaded", label: "Uploaded" },
  { value: "downloaded_session", label: "Session Downloaded" },
  { value: "uploaded_session", label: "Session Uploaded" },
  { value: "amount_left", label: "Remaining" },
  { value: "time_active", label: "Time Active" },
  { value: "seeding_time", label: "Seeding Time" },
  { value: "save_path", label: "Save Path" },
  { value: "completed", label: "Completed" },
  { value: "ratio_limit", label: "Ratio Limit" },
  { value: "seen_complete", label: "Last Seen Complete" },
  { value: "last_activity", label: "Last Activity" },
  { value: "availability", label: "Availability" },
  { value: "infohash_v1", label: "Info Hash v1" },
  { value: "infohash_v2", label: "Info Hash v2" },
  { value: "reannounce", label: "Reannounce In" },
  { value: "private", label: "Private" },
] as const

export type TorrentSortOptionValue = typeof TORRENT_SORT_OPTIONS[number]["value"]

export function getDefaultSortOrder(field: TorrentSortOptionValue): "asc" | "desc" {
  const columnType = getColumnType(field)
  return columnType === "string" || columnType === "enum" ? "asc" : "desc"
}
