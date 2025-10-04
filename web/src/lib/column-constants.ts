/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

export const SIZE_COLUMNS = [
  "size",
  "total_size",
  "downloaded",
  "uploaded",
  "downloaded_session",
  "uploaded_session",
  "amount_left",
  "completed",
] as const satisfies readonly (keyof Torrent)[]

export const DATE_COLUMNS = [
  "added_on",
  "completion_on",
  "seen_complete",
  "last_activity",
] as const satisfies readonly (keyof Torrent)[]

export const DURATION_COLUMNS = [
  "eta",
  "time_active",
  "seeding_time",
  "reannounce",
] as const satisfies readonly (keyof Torrent)[]