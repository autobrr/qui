/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type TransferHistoryBucket = "hour" | "day" | "total"

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
  torrent?: TransferHistoryTorrent
}

export interface TransferHistoryTorrent {
  name: string
  trackerGroup: string
  size: number
  deleted: boolean
  statisticsCrossSeed: boolean
  uploadFirst24h: number | null
}

export type TransferHistoryGrouping = "tracker" | "category" | "torrent" | "releaseGroup" | "resolution" | "source" | "contentType"

export type TableGrouping = Exclude<TransferHistoryGrouping, "torrent">

export interface TransferHistoryQuery {
  from?: Date
  to: Date
  bucket: TransferHistoryBucket
  groupBy: TransferHistoryGrouping
  group?: string
  groupOf?: TransferHistoryGrouping
  limit?: number
  timeZone?: string
}

export interface TransferHistoryInstance {
  instanceId: number
  name: string
  firstRecordedAt: string
}

export interface LiveSamples {
  now: string
  instances: { instanceId: number; samples: { time: string; up: number; down: number }[] }[]
}

export interface TransferHistoryLibraryRow {
  day: string
  torrents?: number
  totalSize?: number
  uniqueSize?: number
  added: number
  completed: number
  deleted: number
}

export interface LiveTrackerGroup {
  group: string
  torrents: number
  size: number
  uploaded: number
}

export type TransferHistoryCauseKind = "automation" | "user" | "backupRestore" | "clientProxy" | "outside"

export interface TransferHistoryDeleteCause {
  kind: TransferHistoryCauseKind
  name?: string
}

export interface TransferHistoryDeleteEvent {
  time: string
  hash: string
  name: string
  trackerGroup: string
  uploaded: number
  downloaded: number
  ratio?: number
  seedingSeconds?: number
  cause: TransferHistoryDeleteCause
}

export type TransferHistoryCauseFilter = "" | "automation" | "user" | "other"

export interface TransferHistoryDeletesQuery {
  from?: Date
  to: Date
  cause: TransferHistoryCauseFilter
  limit: number
  offset: number
}

export interface TransferHistoryDeletesAnswer {
  deletes: TransferHistoryDeleteEvent[]
  counts: { automation: number; user: number; other: number }
}
