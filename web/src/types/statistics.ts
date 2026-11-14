/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type TransferHistoryBucket = "hour" | "day" | "total"

/** The bucket of a row. A year row holds the days of a torrent in a UTC year that qui folded. */
export type TransferHistoryWidth = TransferHistoryBucket | "year"

export interface TransferHistoryBytes {
  uploaded: number
  downloaded: number
  statisticsCrossSeedUploaded: number
}

export interface TransferHistoryRow extends TransferHistoryBytes {
  group: string
  start?: string
  width: TransferHistoryWidth
  gapSeconds: number
  baseline?: TransferHistoryBytes
  /** Set on the rows of the grouping by torrent, whose group is the hash. */
  torrent?: TransferHistoryTorrent
}

export interface TransferHistoryTorrent {
  name: string
  trackerGroup: string
  deleted: boolean
  statisticsCrossSeed: boolean
  /** Null when qui first saw the torrent after its first 24 hours. */
  uploadFirst24h: number | null
}

export type TransferHistoryGrouping = "tracker" | "category" | "torrent" | "releaseGroup" | "resolution" | "source" | "contentType"

/** A grouping of the table of the Statistics page. */
export type TableGrouping = Exclude<TransferHistoryGrouping, "torrent">

export interface TransferHistoryQuery {
  from?: Date
  to: Date
  bucket: TransferHistoryBucket
  groupBy: TransferHistoryGrouping
  /** Limits the rows to one group of groupOf. "" is the group of the torrents with no category, or with no value of the release attribute. */
  group?: string
  /** The grouping of group. The default is groupBy. Only the grouping by torrent takes another one. */
  groupOf?: TransferHistoryGrouping
  /** Keeps the groups with the most upload, in the order of that upload. */
  limit?: number
  /** IANA name of the zone whose midnights start the day rows of Tracker groups. The default is UTC. */
  timeZone?: string
}

export interface TransferHistoryInstance {
  instanceId: number
  name: string
  firstRecordedAt: string
}

/** The torrents of one Tracker group that are in qBittorrent now. uploaded is their lifetime upload. */
export interface LiveTrackerGroup {
  group: string
  torrents: number
  size: number
  uploaded: number
}
