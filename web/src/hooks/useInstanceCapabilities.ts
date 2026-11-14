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

// The capabilities query of one instance. Every reader shares this key, so
// mounting another reader sends no new request.
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

// useInstancesSupport answers no until each instance has answered. Undefined
// instanceIds means every active instance. A torrent row mounts it, so it sends
// a request only for an instance with no capabilities loaded, and it never
// loads the instance list.
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

// refreshChangedCapabilities asks an instance for its capabilities again when
// the torrent list reports a Web API version other than the loaded one, as
// after a qBittorrent upgrade or downgrade.
export function refreshChangedCapabilities(
  queryClient: QueryClient,
  source: Pick<TorrentResponse, "webAPIVersions">
) {
  for (const [instanceId, version] of Object.entries(source.webAPIVersions ?? {})) {
    const queryKey = ["instance-capabilities", Number(instanceId)]
    const loaded = queryClient.getQueryData<InstanceCapabilities>(queryKey)
    if (loaded && (loaded.webAPIVersion ?? "") !== version) {
      // Each stream frame carries the version: keep a running request.
      void queryClient.invalidateQueries({ queryKey, exact: true }, { cancelRefetch: false })
    }
  }
}
