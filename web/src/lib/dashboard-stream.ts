/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentCounts } from "@/types"

/**
 * Dashboard streams intentionally omit torrent counts on lightweight ticks. Treat
 * an omitted primary counts field as "no update" so hydrated tracker metadata
 * from REST/cache does not disappear as soon as the connected stream takes over.
 */
export function resolveDashboardTorrentCounts(
  primaryCounts: TorrentCounts | undefined,
  fallbackCounts: TorrentCounts | undefined
) {
  return primaryCounts ?? fallbackCounts
}
