/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useMemo, useRef, type SetStateAction } from "react"
import type { TorrentFilters } from "@/types"

import { useClientSetting } from "@/lib/client-settings"

type GlobalFilters = Pick<TorrentFilters, "status" | "excludeStatus">
type InstanceFilters = Omit<TorrentFilters, "status" | "excludeStatus">

const DEFAULT_GLOBAL: GlobalFilters = { status: [], excludeStatus: [] }
const DEFAULT_INSTANCE: InstanceFilters = {
  categories: [],
  excludeCategories: [],
  tags: [],
  excludeTags: [],
  trackers: [],
  excludeTrackers: [],
  expr: "",
}

const parseGlobalFilters = (raw: string): GlobalFilters => ({ ...DEFAULT_GLOBAL, ...JSON.parse(raw) })

const parseInstanceFilters = (raw: string): InstanceFilters => ({ ...DEFAULT_INSTANCE, ...JSON.parse(raw) })

export function usePersistedFilters(instanceId: number) {
  const [globalFilters, setGlobalFilters] = useClientSetting<GlobalFilters>("qui-filters-global", {
    defaultValue: DEFAULT_GLOBAL,
    parse: parseGlobalFilters,
  })
  const [instanceFilters, setInstanceFilters] = useClientSetting<InstanceFilters>(`qui-filters-${instanceId}`, {
    defaultValue: DEFAULT_INSTANCE,
    parse: parseInstanceFilters,
  })

  const filters = useMemo<TorrentFilters>(
    () => ({ ...globalFilters, ...instanceFilters }),
    [globalFilters, instanceFilters]
  )

  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const setFilters = useCallback(
    (next: SetStateAction<TorrentFilters>) => {
      const resolved = typeof next === "function" ? next(filtersRef.current) : next
      // Keep every instance-scoped field, including the derived
      // expandedCategories the sidebar computes for subcategory requests.
      const { status, excludeStatus, ...instance } = resolved
      setGlobalFilters({ status, excludeStatus })
      setInstanceFilters(instance)
    },
    [setGlobalFilters, setInstanceFilters]
  )

  return [filters, setFilters] as const
}
