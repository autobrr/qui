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

export function toViewFilters(filters: Partial<TorrentFilters> | undefined): TorrentFilters {
  const normalized = { expr: filters?.expr || "" } as TorrentFilters
  for (const key of FILTER_ARRAY_KEYS) {
    normalized[key] = [...(filters?.[key] ?? [])].sort()
  }
  return normalized
}

export function filterViewsEqual(a: Partial<TorrentFilters> | undefined, b: Partial<TorrentFilters> | undefined): boolean {
  return JSON.stringify(toViewFilters(a)) === JSON.stringify(toViewFilters(b))
}

export const EMPTY_SIDEBAR_FILTERS: TorrentFilters = toViewFilters(undefined)

export function hasSidebarFilters(filters: Partial<TorrentFilters>): boolean {
  return FILTER_ARRAY_KEYS.some(key => (filters[key]?.length ?? 0) > 0) || Boolean(filters.expr?.trim())
}
