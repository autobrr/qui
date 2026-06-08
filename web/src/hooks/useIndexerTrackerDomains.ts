/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

interface UseIndexerTrackerDomainsOptions {
  enabled?: boolean
  staleTimeMs?: number
}

/**
 * Fetches tracker domains derived from qui's configured (enabled) indexers.
 * These are global (not instance-scoped) and let users select trackers that have
 * no active torrents. Errors resolve to an empty list so the caller degrades
 * gracefully when indexers aren't configured.
 */
export function useIndexerTrackerDomains(options: UseIndexerTrackerDomainsOptions = {}) {
  const { enabled = true, staleTimeMs = 1000 * 60 * 5 } = options

  return useQuery({
    queryKey: ["indexer-tracker-domains"],
    queryFn: async () => {
      try {
        return await api.getIndexerTrackerDomains()
      } catch {
        return []
      }
    },
    staleTime: staleTimeMs,
    enabled,
    retry: false,
  })
}
