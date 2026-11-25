/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

interface UseInstanceTrackersOptions {
  enabled?: boolean
  staleTimeMs?: number
}

export function instanceTrackersQueryOptions(instanceId: number, staleTimeMs = 1000 * 60 * 5) {
  return {
    queryKey: ["instance-trackers", instanceId],
    queryFn: () => api.getActiveTrackers(instanceId),
    staleTime: staleTimeMs,
  }
}

export function useInstanceTrackers(instanceId: number, options: UseInstanceTrackersOptions = {}) {
  const { enabled = true, staleTimeMs } = options

  return useQuery({
    ...instanceTrackersQueryOptions(instanceId, staleTimeMs),
    enabled: Boolean(enabled && instanceId),
  })
}
