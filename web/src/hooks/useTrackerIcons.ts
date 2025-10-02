/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"

/**
 * Hook for fetching all cached tracker icons
 * Returns a map of tracker hostnames to base64-encoded data URLs
 */
export function useTrackerIcons() {
  const query = useQuery<Record<string, string>>({
    queryKey: ["tracker-icons"],
    queryFn: async () => {
      return api.getTrackerIcons()
    },
    staleTime: 300000, // 5 minutes - icons don't change often
    gcTime: 1800000, // Keep in cache for 30 minutes
    refetchInterval: 300000, // Refetch every 5 minutes
    refetchIntervalInBackground: false,
    placeholderData: (previousData) => previousData,
  })

  return query
}
