/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences, Category } from "@/types"

interface InstanceMetadata {
  categories: Record<string, Category>
  tags: string[]
  preferences: AppPreferences
}

/**
 * Shared hook for fetching instance metadata (categories, tags, preferences)
 * This prevents duplicate API calls when multiple components need the same data
 * Note: Counts are now included in the torrents response, so we don't fetch them separately
 */
export function useInstanceMetadata(instanceId: number) {
  const query = useQuery<InstanceMetadata>({
    queryKey: ["instance-metadata", instanceId],
    enabled: instanceId > 0,
    queryFn: async () => {
      // Fetch metadata in parallel for efficiency
      const [categories, tags, preferences] = await Promise.all([
        api.getCategories(instanceId),
        api.getTags(instanceId),
        api.getInstancePreferences(instanceId),
      ])

      return { categories, tags, preferences }
    },
    staleTime: 60000, // 1 minute - metadata doesn't change often
    gcTime: 1800000, // Keep in cache for 30 minutes to support cross-instance navigation
    refetchInterval: 30000, // Refetch every 30 seconds
    refetchIntervalInBackground: false, // Don't refetch when tab is not active
    // IMPORTANT: Keep showing previous data while fetching new data
    placeholderData: (previousData) => previousData,
  })

  return query
}
