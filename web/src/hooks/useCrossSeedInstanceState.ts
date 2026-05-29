/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

import { useActivityStream } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"

export type CrossSeedInstanceState = Record<number, {
  rssEnabled?: boolean
  rssRunning?: boolean
  searchRunning?: boolean
}>

export type CrossSeedInstanceStateResult = {
  state: CrossSeedInstanceState
  isLoading: boolean
  isError: boolean
  error: Error | null
}

export function useCrossSeedInstanceState(): CrossSeedInstanceStateResult {
  // Keep the shared SSE connection open app-wide (this hook backs the Sidebar badge).
  // Events invalidate ["cross-seed","status"] and ["cross-seed","search-status"].
  useActivityStream()

  const settingsQuery = useQuery({
    queryKey: ["cross-seed", "settings"],
    queryFn: () => api.getCrossSeedSettings(),
    staleTime: 5 * 60 * 1000,
    gcTime: 10 * 60 * 1000,
  })

  const rssEnabled = settingsQuery.data?.enabled ?? false

  const statusQuery = useQuery({
    queryKey: ["cross-seed", "status"],
    queryFn: () => api.getCrossSeedStatus(),
    refetchInterval: false,
    staleTime: 10_000,
    enabled: rssEnabled,
  })

  // Poll frequently only while a search is actively running for smooth progress; rely on events otherwise
  const searchStatusQuery = useQuery({
    queryKey: ["cross-seed", "search-status"],
    queryFn: () => api.getCrossSeedSearchStatus(),
    refetchInterval: (query) => query.state.data?.running ? 5_000 : false,
    staleTime: 3_000,
  })

  const state = useMemo(() => {
    const settings = settingsQuery.data
    const crossSeedStatus = statusQuery.data
    const crossSeedSearchStatus = searchStatusQuery.data

    const rssEnabled = settings?.enabled ?? false
    const rssRunning = crossSeedStatus?.running ?? false
    const rssTargetIds = crossSeedStatus?.settings?.targetInstanceIds ?? []
    const searchRunning = crossSeedSearchStatus?.running ?? false
    const searchInstanceId = crossSeedSearchStatus?.run?.instanceId

    const state: CrossSeedInstanceState = {}

    if (rssEnabled) {
      for (const id of rssTargetIds) {
        state[id] = { rssEnabled, rssRunning }
      }
    }

    if (searchRunning && searchInstanceId) {
      state[searchInstanceId] = {
        ...state[searchInstanceId],
        searchRunning: true,
      }
    }

    return state
  }, [settingsQuery.data, statusQuery.data, searchStatusQuery.data])

  return {
    state,
    isLoading: settingsQuery.isLoading || statusQuery.isLoading || searchStatusQuery.isLoading,
    isError: settingsQuery.isError || statusQuery.isError || searchStatusQuery.isError,
    error: settingsQuery.error || statusQuery.error || searchStatusQuery.error,
  }
}
