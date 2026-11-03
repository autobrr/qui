/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, type QueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { InstanceCapabilities, TorrentResponse } from "@/types"

type UseInstanceCapabilitiesOptions = {
  enabled?: boolean
}

export function useInstanceCapabilities(
  instanceId: number | null | undefined,
  options: UseInstanceCapabilitiesOptions = {}
) {
  const shouldEnable = options.enabled ?? true

  return useQuery<InstanceCapabilities>({
    queryKey: ["instance-capabilities", instanceId],
    queryFn: () => api.getInstanceCapabilities(instanceId!),
    enabled: shouldEnable && instanceId !== null && instanceId !== undefined,
    staleTime: 60_000,
    refetchInterval: (query) => {
      const data = query.state.data as InstanceCapabilities | undefined
      if (!data?.webAPIVersion) {
        return 5_000
      }
      return false
    },
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
