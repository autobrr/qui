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

export interface TransferHistoryQuery {
  from?: Date
  to: Date
  bucket: TransferHistoryBucket
  groupBy: "tracker" | "category"
}

export interface TransferHistoryInstance {
  instanceId: number
  name: string
  firstRecordedAt: string
}
