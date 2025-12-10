import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

import { api } from "@/lib/api"

export type CrossSeedInstanceState = Record<number, {
  rssEnabled?: boolean
  rssRunning?: boolean
  searchRunning?: boolean
}>

export function useCrossSeedInstanceState(): CrossSeedInstanceState {
  // Fetch settings to determine if RSS automation is enabled
  // Long stale time since settings rarely change
  const { data: settings } = useQuery({
    queryKey: ["cross-seed", "settings"],
    queryFn: () => api.getCrossSeedSettings(),
    staleTime: 5 * 60 * 1000, // 5 minutes
    gcTime: 10 * 60 * 1000, // 10 minutes
  })

  const rssEnabled = settings?.enabled ?? false

  // Only poll status when RSS automation is enabled
  const { data: crossSeedStatus } = useQuery({
    queryKey: ["cross-seed", "status"],
    queryFn: () => api.getCrossSeedStatus(),
    refetchInterval: rssEnabled ? 30_000 : false,
    staleTime: 10_000,
    enabled: rssEnabled,
  })

  // Fetch search status once to check if running, then poll only while running
  const { data: crossSeedSearchStatus } = useQuery({
    queryKey: ["cross-seed", "search-status"],
    queryFn: () => api.getCrossSeedSearchStatus(),
    refetchInterval: (query) => {
      // Only poll while a search is actively running
      return query.state.data?.running ? 5_000 : false
    },
    staleTime: 3_000,
  })

  return useMemo(() => {
    const rssRunning = crossSeedStatus?.running ?? false
    const rssTargetIds = crossSeedStatus?.settings?.targetInstanceIds ?? []
    const searchRunning = crossSeedSearchStatus?.running ?? false
    const searchInstanceId = crossSeedSearchStatus?.run?.instanceId

    const state: CrossSeedInstanceState = {}

    // Only populate RSS state if RSS is enabled
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
  }, [crossSeedStatus, crossSeedSearchStatus, rssEnabled])
}
