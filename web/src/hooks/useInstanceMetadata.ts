/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences, Category } from "@/types"

export interface InstanceMetadata {
  categories: Record<string, Category>
  tags: string[]
  preferences?: AppPreferences
}

/**
 * Shared hook for fetching instance metadata (categories, tags, preferences)
 * This prevents duplicate API calls when multiple components need the same data
 * Note: Counts are now included in the torrents response, so we don't fetch them separately
 */
export function useInstanceMetadata(instanceId: number) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(() => ["instance-metadata", instanceId] as const, [instanceId])

  const query = useQuery<InstanceMetadata>({
    queryKey,
    queryFn: async () => {
      const categoriesPromise = api.getCategories(instanceId)
      const tagsPromise = api.getTags(instanceId)

      const preferencesPromise = api.getInstancePreferences(instanceId)

      const [categories, tags, preferences] = await Promise.all([
        categoriesPromise,
        tagsPromise,
        preferencesPromise,
      ])

      return { categories, tags, preferences }
    },
    initialData: () => queryClient.getQueryData<InstanceMetadata>(queryKey),
    staleTime: 60000, // 1 minute - metadata doesn't change often
    gcTime: 1800000, // Keep in cache for 30 minutes to support cross-instance navigation
    refetchInterval: 30000, // Refetch every 30 seconds
    refetchIntervalInBackground: false, // Don't refetch when tab is not active
    // IMPORTANT: Keep showing previous data while fetching new data
    placeholderData: (previousData) => previousData,
  })

  return query
}
