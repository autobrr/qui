/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferHistoryInstance } from "./statistics"

export type DetailRetentionDays = 0 | 90 | 400

export interface TransferHistorySettings {
  recording: boolean
  detailRetentionDays: DetailRetentionDays
}

export interface TransferHistorySettingsStatus {
  instances: TransferHistoryInstance[]
  historyBytes: number
  settings: TransferHistorySettings
}
