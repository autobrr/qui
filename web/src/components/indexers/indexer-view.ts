/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorznabIndexer } from "@/types"

export interface IndexerFilters {
  status: "all" | "enabled" | "disabled"
  test: "all" | "ok" | "error" | "untested"
  backend: "all" | TorznabIndexer["backend"]
}

export interface IndexerSort {
  field: "name" | "backend" | "priority" | "status"
  direction: "asc" | "desc"
}

export const NO_FILTERS: IndexerFilters = { status: "all", test: "all", backend: "all" }
export const DEFAULT_SORT: IndexerSort = { field: "priority", direction: "asc" }

export function activeFilterCount(filters: IndexerFilters): number {
  return [filters.status, filters.test, filters.backend].filter((value) => value !== "all").length
}

function testOutcome(indexer: TorznabIndexer): IndexerFilters["test"] {
  return indexer.last_test_status === "ok" || indexer.last_test_status === "error" ? indexer.last_test_status : "untested"
}

const COMPARE: Record<IndexerSort["field"], (a: TorznabIndexer, b: TorznabIndexer) => number> = {
  name: (a, b) => a.name.localeCompare(b.name),
  backend: (a, b) => a.backend.localeCompare(b.backend),
  priority: (a, b) => a.priority - b.priority,
  status: (a, b) => Number(a.enabled) - Number(b.enabled),
}

/** The indexers that pass the filters, in the sort order. */
export function viewIndexers(indexers: readonly TorznabIndexer[], filters: IndexerFilters, sort: IndexerSort): TorznabIndexer[] {
  const sign = sort.direction === "asc" ? 1 : -1
  return indexers
    .filter((indexer) =>
      (filters.status === "all" || indexer.enabled === (filters.status === "enabled")) &&
      (filters.test === "all" || testOutcome(indexer) === filters.test) &&
      (filters.backend === "all" || indexer.backend === filters.backend)
    )
    .sort((a, b) => sign * COMPARE[sort.field](a, b))
}
