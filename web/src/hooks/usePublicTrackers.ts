/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { PruneMode, PublicTrackerSettings, PublicTrackerSettingsInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

const QUERY_KEY = ["public-tracker-settings"]

/**
 * Hook for fetching public tracker settings
 */
export function usePublicTrackerSettings() {
  return useQuery<PublicTrackerSettings>({
    queryKey: QUERY_KEY,
    queryFn: () => api.getPublicTrackerSettings(),
    staleTime: 60000, // 1 minute
    gcTime: 300000, // 5 minutes
  })
}

/**
 * Hook for updating public tracker settings
 */
export function useUpdatePublicTrackerSettings() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: PublicTrackerSettingsInput) => api.updatePublicTrackerSettings(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
  })
}

/**
 * Hook for refreshing the public tracker list from the configured URL
 */
export function useRefreshPublicTrackerList() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.refreshPublicTrackerList(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
  })
}

/**
 * Hook for executing a public tracker action on selected torrents
 */
export function usePublicTrackerAction(instanceId: number) {
  return useMutation({
    mutationFn: ({ hashes, pruneMode }: { hashes: string[]; pruneMode: PruneMode }) =>
      api.executePublicTrackerAction(instanceId, hashes, pruneMode),
  })
}
