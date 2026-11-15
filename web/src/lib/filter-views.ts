/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentFilters } from "@/types"

const FILTER_ARRAY_KEYS = [
  "status",
  "excludeStatus",
  "categories",
  "excludeCategories",
  "tags",
  "excludeTags",
  "trackers",
  "excludeTrackers",
] as const

/**
 * Normalizes a filter object into the snapshot a saved view stores: every array
 * key present and sorted, `expr` normalized, and the derived `expandedCategories`
 * / `expandedExcludeCategories` fields dropped (FilterSidebar recomputes those).
 *
 * The result is also what applying a view feeds back in, so missing keys reset
 * instead of merging with whatever was selected before.
 */
export function toViewFilters(filters: Partial<TorrentFilters> | undefined): TorrentFilters {
  const normalized = { expr: filters?.expr || "" } as TorrentFilters
  for (const key of FILTER_ARRAY_KEYS) {
    normalized[key] = [...(filters?.[key] ?? [])].sort()
  }
  return normalized
}

/** Deep-equality of two filter selections, ignoring array order and expanded* fields. */
export function filterViewsEqual(a: Partial<TorrentFilters> | undefined, b: Partial<TorrentFilters> | undefined): boolean {
  // ponytail: key order is fixed by toViewFilters, so stringify is a safe deep compare
  return JSON.stringify(toViewFilters(a)) === JSON.stringify(toViewFilters(b))
}

/** The selection after the user clears every sidebar filter. */
export const EMPTY_SIDEBAR_FILTERS: TorrentFilters = toViewFilters(undefined)

/** Reports whether any sidebar filter is set. A blank expression filters nothing. */
export function hasSidebarFilters(filters: Partial<TorrentFilters>): boolean {
  return FILTER_ARRAY_KEYS.some(key => (filters[key]?.length ?? 0) > 0) || Boolean(filters.expr?.trim())
}
