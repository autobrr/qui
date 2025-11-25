import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

import { api } from "@/lib/api"

export type CrossSeedInstanceState = Record<number, {
  rssEnabled?: boolean
  rssRunning?: boolean
  searchRunning?: boolean
}>

export function useCrossSeedInstanceState(): CrossSeedInstanceState {
  const { data: crossSeedStatus } = useQuery({
    queryKey: ["cross-seed", "status"],
    queryFn: () => api.getCrossSeedStatus(),
    refetchInterval: 30_000,
    staleTime: 10_000,
  })

  const { data: crossSeedSearchStatus } = useQuery({
    queryKey: ["cross-seed", "search-status"],
    queryFn: () => api.getCrossSeedSearchStatus(),
    refetchInterval: 5_000,
    staleTime: 3_000,
  })

  return useMemo(() => {
    const rssEnabled = crossSeedStatus?.settings?.enabled ?? false
    const rssRunning = crossSeedStatus?.running ?? false
    const rssTargetIds = crossSeedStatus?.settings?.targetInstanceIds ?? []
    const searchRunning = crossSeedSearchStatus?.running ?? false
    const searchInstanceId = crossSeedSearchStatus?.run?.instanceId

    const state: CrossSeedInstanceState = {}

    for (const id of rssTargetIds) {
      state[id] = { rssEnabled, rssRunning }
    }

    if (searchRunning && searchInstanceId) {
      state[searchInstanceId] = {
        ...state[searchInstanceId],
        searchRunning: true,
      }
    }

    return state
  }, [crossSeedStatus, crossSeedSearchStatus])
}
