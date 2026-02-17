/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"
import type { UserDefinedView } from "@/types"

/**
 * Hook for fetching user-defined views for an instance
 * Returns a list of user-defined views
 */
export function useUserDefinedViews(instanceId: number) {
  const query = useQuery<UserDefinedView[]>({
    queryKey: ["user-defined-views", instanceId],
    enabled: instanceId > 0,
    queryFn: async () => {
      return await api.getUserDefinedViews(instanceId)
    },
    staleTime: 60000, // 1 minute - views don't change often
    gcTime: 1800000, // Keep in cache for 30 minutes
    refetchInterval: 30000, // Refetch every 30 seconds
    refetchIntervalInBackground: false,
    placeholderData: (previousData) => previousData,
  })

  return query
}
