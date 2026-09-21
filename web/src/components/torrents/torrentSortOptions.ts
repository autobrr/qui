/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getColumnType } from "@/lib/column-filter-utils"

// labelKey is a key in the torrents namespace.
export const TORRENT_SORT_OPTIONS = [
  { value: "added_on", labelKey: "sort.options.addedOn" },
  { value: "name", labelKey: "tableColumns.name" },
  { value: "size", labelKey: "tableColumns.size" },
  // { value: "total_size", labelKey: "tableColumns.totalSize" },
  { value: "progress", labelKey: "tableColumns.progress" },
  { value: "state", labelKey: "tableColumns.status" },
  { value: "priority", labelKey: "tableColumns.priority" },
  { value: "num_seeds", labelKey: "tableColumns.seeds" },
  { value: "num_leechs", labelKey: "detailsPanel.labels.leechers" },
  { value: "dlspeed", labelKey: "detailsPanel.labels.downloadSpeed" },
  { value: "upspeed", labelKey: "detailsPanel.labels.uploadSpeed" },
  { value: "eta", labelKey: "tableColumns.eta" },
  { value: "ratio", labelKey: "tableColumns.ratio" },
  { value: "popularity", labelKey: "tableColumns.popularity" },
  { value: "category", labelKey: "tableColumns.category" },
  { value: "tags", labelKey: "tableColumns.tags" },
  { value: "completion_on", labelKey: "tableColumns.completedOn" },
  { value: "tracker", labelKey: "tableColumns.tracker" },
  { value: "dl_limit", labelKey: "sort.options.downloadLimit" },
  { value: "up_limit", labelKey: "sort.options.uploadLimit" },
  { value: "downloaded", labelKey: "tableColumns.downloaded" },
  { value: "uploaded", labelKey: "tableColumns.uploaded" },
  { value: "downloaded_session", labelKey: "sort.options.sessionDownloaded" },
  { value: "uploaded_session", labelKey: "sort.options.sessionUploaded" },
  { value: "amount_left", labelKey: "tableColumns.amountLeft" },
  { value: "time_active", labelKey: "tableColumns.timeActive" },
  { value: "seeding_time", labelKey: "tableColumns.seedingTime" },
  { value: "save_path", labelKey: "tableColumns.savePath" },
  { value: "completed", labelKey: "tableColumns.completed" },
  { value: "ratio_limit", labelKey: "tableColumns.ratioLimit" },
  { value: "seen_complete", labelKey: "sort.options.lastSeenComplete" },
  { value: "last_activity", labelKey: "tableColumns.lastActivity" },
  { value: "availability", labelKey: "tableColumns.availability" },
  { value: "infohash_v1", labelKey: "tableColumns.infohashV1" },
  { value: "infohash_v2", labelKey: "tableColumns.infohashV2" },
  { value: "reannounce", labelKey: "sort.options.reannounceIn" },
  { value: "private", labelKey: "tableColumns.private" },
] as const

export type TorrentSortOptionValue = typeof TORRENT_SORT_OPTIONS[number]["value"]

export function getDefaultSortOrder(field: TorrentSortOptionValue): "asc" | "desc" {
  const columnType = getColumnType(field)
  return columnType === "string" || columnType === "enum" ? "asc" : "desc"
}
