/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQueries, useQuery, type QueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { InstanceCapabilities, InstanceResponse, TorrentResponse } from "@/types"

type UseInstanceCapabilitiesOptions = {
  enabled?: boolean
}

export function instanceCapabilitiesQuery(instanceId: number) {
  return {
    queryKey: ["instance-capabilities", instanceId],
    queryFn: () => api.getInstanceCapabilities(instanceId),
    staleTime: 60_000,
  }
}

export function useInstanceCapabilities(
  instanceId: number | null | undefined,
  options: UseInstanceCapabilitiesOptions = {}
) {
  const shouldEnable = options.enabled ?? true

  return useQuery<InstanceCapabilities>({
    ...instanceCapabilitiesQuery(instanceId!),
    enabled: shouldEnable && instanceId !== null && instanceId !== undefined,
    refetchInterval: (query) => {
      const data = query.state.data as InstanceCapabilities | undefined
      if (!data?.webAPIVersion) {
        return 5_000
      }
      return false
    },
  })
}

export type CapabilityFlag = {
  [K in keyof InstanceCapabilities]-?: InstanceCapabilities[K] extends boolean | undefined ? K : never
}[keyof InstanceCapabilities]

export function useInstancesSupport(
  instanceIds: readonly number[] | undefined,
  capability: CapabilityFlag
): boolean {
  const { data: activeInstanceIds } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    enabled: false,
    select: (instances: InstanceResponse[]) => instances.filter(instance => instance.isActive).map(instance => instance.id),
  })
  const ids = instanceIds ?? activeInstanceIds ?? []

  return useQueries({
    queries: ids.map(id => ({ ...instanceCapabilitiesQuery(id), refetchOnMount: false })),
    combine: results => results.length > 0 && results.every(result => result.data?.[capability] === true),
  })
}

export function refreshChangedCapabilities(
  queryClient: QueryClient,
  source: Pick<TorrentResponse, "webAPIVersions">
) {
  for (const [instanceId, version] of Object.entries(source.webAPIVersions ?? {})) {
    const queryKey = ["instance-capabilities", Number(instanceId)]
    const loaded = queryClient.getQueryData<InstanceCapabilities>(queryKey)
    if (loaded && (loaded.webAPIVersion ?? "") !== version) {
      void queryClient.invalidateQueries({ queryKey, exact: true }, { cancelRefetch: false })
    }
  }
}
