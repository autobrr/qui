import { api } from "@/lib/api"
import { useQueries } from "@tanstack/react-query"
import { useMemo } from "react"

export interface UnifiedActionInstance {
  id: number
  name: string
  connected: boolean
}

interface UseUnifiedActionInstancesProps {
  activeInstances: UnifiedActionInstance[]
  effectiveUnifiedInstanceIds: readonly number[]
  enabled: boolean
}

export function useUnifiedActionInstances({
  activeInstances,
  effectiveUnifiedInstanceIds,
  enabled,
}: UseUnifiedActionInstancesProps) {
  const unifiedScopeInstances = useMemo(
    () => activeInstances.filter(instance => effectiveUnifiedInstanceIds.includes(instance.id)),
    [activeInstances, effectiveUnifiedInstanceIds]
  )
  const unifiedManageableInstances = useMemo(
    () => unifiedScopeInstances.filter(instance => instance.id > 0),
    [unifiedScopeInstances]
  )
  const unifiedCapabilitiesResults = useQueries({
    queries: unifiedManageableInstances.map((instance) => ({
      queryKey: ["instance-capabilities", instance.id],
      queryFn: () => api.getInstanceCapabilities(instance.id),
      staleTime: 60_000,
      enabled,
    })),
  })
  const unifiedTorrentCreationInstances = useMemo(
    () => unifiedManageableInstances.filter((_instance, i) =>
      unifiedCapabilitiesResults[i]?.data?.supportsTorrentCreation === true
    ),
    [unifiedManageableInstances, unifiedCapabilitiesResults]
  )

  return {
    unifiedManageableInstances,
    unifiedTorrentCreationInstances,
  }
}
