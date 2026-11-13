/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type TransferHistoryBucket = "hour" | "day" | "total"

export interface TransferHistoryBytes {
  uploaded: number
  downloaded: number
  statisticsCrossSeedUploaded: number
}

export interface TransferHistoryRow extends TransferHistoryBytes {
  group: string
  start?: string
  width: TransferHistoryBucket
  gapSeconds: number
  baseline?: TransferHistoryBytes
}

export type TransferHistoryGrouping = "tracker" | "category"

export interface TransferHistoryQuery {
  from?: Date
  to: Date
  bucket: TransferHistoryBucket
  groupBy: TransferHistoryGrouping
  /** Limits the rows to one group. "" is the group of the torrents with no category. */
  group?: string
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
