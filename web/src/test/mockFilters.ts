/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentFilters } from "@/types"

/**
 * Build a TorrentFilters object with empty defaults for unit tests. Override
 * only the fields a test exercises (most commonly `expr`).
 */
export function makeFilters(overrides: Partial<TorrentFilters> = {}): TorrentFilters {
  return {
    status: [],
    excludeStatus: [],
    categories: [],
    excludeCategories: [],
    tags: [],
    excludeTags: [],
    trackers: [],
    excludeTrackers: [],
    ...overrides,
  }
}
