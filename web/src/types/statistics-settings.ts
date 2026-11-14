/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferHistoryInstance } from "./statistics"

/** Days that qui keeps the hourly totals and the daily bytes of each torrent. 0 keeps them forever. */
export type DetailRetentionDays = 0 | 90 | 400

export interface TransferHistorySettings {
  recording: boolean
  detailRetentionDays: DetailRetentionDays
}

/** The part of the transfer history status that the Statistics settings show. */
export interface TransferHistorySettingsStatus {
  instances: TransferHistoryInstance[]
  historyBytes: number
  settings: TransferHistorySettings
}
