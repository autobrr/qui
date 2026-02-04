/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { InstanceCapabilities } from "@/types"

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
    staleTime: 300000,
    refetchInterval: (query) => {
      const data = query.state.data as InstanceCapabilities | undefined
      if (!data?.webAPIVersion) {
        return 5_000
      }
      return false
    },
  })
}
