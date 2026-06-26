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

/**
 * Fetches and caches the active tracker domains for an instance.
 * This hook centralizes the query so multiple consumers share the same cache entry.
 */
export function useInstanceTrackers(instanceId: number, options: UseInstanceTrackersOptions = {}) {
  const { enabled = true, staleTimeMs = 1000 * 60 * 5 } = options

  return useQuery({
    queryKey: ["instance-trackers", instanceId],
    queryFn: () => api.getActiveTrackers(instanceId),
    staleTime: staleTimeMs,
    enabled: Boolean(enabled && instanceId),
  })
}
