/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getColumnType } from "@/lib/column-filter-utils"

export const TORRENT_SORT_OPTIONS = [
  { value: "added_on", labelKey: "torrentTableColumns.headers.added", fallbackLabel: "Recently Added" },
  { value: "name", labelKey: "torrentTableColumns.headers.name", fallbackLabel: "Name" },
  { value: "size", labelKey: "torrentTableColumns.headers.size", fallbackLabel: "Size" },
  // { value: "total_size", labelKey: "torrentTableColumns.headers.totalSize", fallbackLabel: "Total Size" },
  { value: "progress", labelKey: "torrentTableColumns.headers.progress", fallbackLabel: "Progress" },
  { value: "state", labelKey: "torrentTableColumns.headers.status", fallbackLabel: "Status" },
  { value: "priority", labelKey: "torrentTableColumns.headers.priority", fallbackLabel: "Priority" },
  { value: "num_seeds", labelKey: "torrentTableColumns.headers.seeds", fallbackLabel: "Seeds" },
  { value: "num_leechs", labelKey: "torrentTableColumns.headers.peers", fallbackLabel: "Leechers" },
  { value: "dlspeed", labelKey: "torrentTableColumns.headers.downSpeed", fallbackLabel: "Download Speed" },
  { value: "upspeed", labelKey: "torrentTableColumns.headers.upSpeed", fallbackLabel: "Upload Speed" },
  { value: "eta", labelKey: "torrentTableColumns.headers.eta", fallbackLabel: "ETA" },
  { value: "ratio", labelKey: "torrentTableColumns.headers.ratio", fallbackLabel: "Ratio" },
  { value: "popularity", labelKey: "torrentTableColumns.headers.popularity", fallbackLabel: "Popularity" },
  { value: "category", labelKey: "torrentTableColumns.headers.category", fallbackLabel: "Category" },
  { value: "tags", labelKey: "torrentTableColumns.headers.tags", fallbackLabel: "Tags" },
  { value: "completion_on", labelKey: "torrentTableColumns.headers.completedOn", fallbackLabel: "Completed On" },
  { value: "tracker", labelKey: "torrentTableColumns.headers.tracker", fallbackLabel: "Tracker" },
  { value: "dl_limit", labelKey: "torrentTableColumns.headers.downLimit", fallbackLabel: "Download Limit" },
  { value: "up_limit", labelKey: "torrentTableColumns.headers.upLimit", fallbackLabel: "Upload Limit" },
  { value: "downloaded", labelKey: "torrentTableColumns.headers.downloaded", fallbackLabel: "Downloaded" },
  { value: "uploaded", labelKey: "torrentTableColumns.headers.uploaded", fallbackLabel: "Uploaded" },
  { value: "downloaded_session", labelKey: "torrentTableColumns.headers.sessionDownloaded", fallbackLabel: "Session Downloaded" },
  { value: "uploaded_session", labelKey: "torrentTableColumns.headers.sessionUploaded", fallbackLabel: "Session Uploaded" },
  { value: "amount_left", labelKey: "torrentTableColumns.headers.remaining", fallbackLabel: "Remaining" },
  { value: "time_active", labelKey: "torrentTableColumns.headers.timeActive", fallbackLabel: "Time Active" },
  { value: "seeding_time", labelKey: "torrentTableColumns.headers.seedingTime", fallbackLabel: "Seeding Time" },
  { value: "save_path", labelKey: "torrentTableColumns.headers.savePath", fallbackLabel: "Save Path" },
  { value: "completed", labelKey: "torrentTableColumns.headers.completed", fallbackLabel: "Completed" },
  { value: "ratio_limit", labelKey: "torrentTableColumns.headers.ratioLimit", fallbackLabel: "Ratio Limit" },
  { value: "seen_complete", labelKey: "torrentTableColumns.headers.lastSeenComplete", fallbackLabel: "Last Seen Complete" },
  { value: "last_activity", labelKey: "torrentTableColumns.headers.lastActivity", fallbackLabel: "Last Activity" },
  { value: "availability", labelKey: "torrentTableColumns.headers.availability", fallbackLabel: "Availability" },
  { value: "infohash_v1", labelKey: "torrentTableColumns.headers.infoHashV1", fallbackLabel: "Info Hash v1" },
  { value: "infohash_v2", labelKey: "torrentTableColumns.headers.infoHashV2", fallbackLabel: "Info Hash v2" },
  { value: "reannounce", labelKey: "torrentTableColumns.headers.reannounceIn", fallbackLabel: "Reannounce In" },
  { value: "private", labelKey: "torrentTableColumns.headers.private", fallbackLabel: "Private" },
] as const

export type TorrentSortOptionValue = typeof TORRENT_SORT_OPTIONS[number]["value"]

export function getDefaultSortOrder(field: TorrentSortOptionValue): "asc" | "desc" {
  const columnType = getColumnType(field)
  return columnType === "string" || columnType === "enum" ? "asc" : "desc"
}
