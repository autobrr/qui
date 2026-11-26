/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter } from "@/lib/column-filter-utils"

import { useClientSetting } from "@/lib/client-settings"

const NO_FILTERS: ColumnFilter[] = []

const parseColumnFilters = (raw: string): ColumnFilter[] => {
  const parsed = JSON.parse(raw)
  if (!Array.isArray(parsed)) throw new Error("invalid column filters")
  return parsed
}

export function usePersistedColumnFilters(instanceId: number) {
  return useClientSetting<ColumnFilter[]>(`qui-column-filters-${instanceId}`, {
    defaultValue: NO_FILTERS,
    parse: parseColumnFilters,
  })
}
