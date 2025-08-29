/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences } from "@/types"

export function useInstancePreferences(instanceId: number | undefined) {
  const queryClient = useQueryClient()

  const { data: preferences, isLoading, error } = useQuery({
    queryKey: ["instance-preferences", instanceId],
    queryFn: () => instanceId ? api.getInstancePreferences(instanceId) : null,
    enabled: !!instanceId,
    staleTime: 5000, // 5 seconds
    refetchInterval: 60000, // Refetch every minute
    placeholderData: (previousData) => previousData,
  })

  const updateMutation = useMutation({
    mutationFn: (preferences: Partial<AppPreferences>) => {
      if (!instanceId) throw new Error("No instance ID")
      return api.updateInstancePreferences(instanceId, preferences)
    },
    onMutate: async (newPreferences) => {
      // Cancel outgoing refetches
      await queryClient.cancelQueries({
        queryKey: ["instance-preferences", instanceId],
      })

      // Snapshot previous value
      const previousPreferences = queryClient.getQueryData<AppPreferences>(
        ["instance-preferences", instanceId]
      )

      // Optimistically update
      if (previousPreferences) {
        queryClient.setQueryData(
          ["instance-preferences", instanceId],
          { ...previousPreferences, ...newPreferences }
        )
      }

      return { previousPreferences }
    },
    onError: (_err, _newPreferences, context) => {
      // Rollback on error
      if (context?.previousPreferences) {
        queryClient.setQueryData(
          ["instance-preferences", instanceId],
          context.previousPreferences
        )
      }
    },
    onSuccess: () => {
      // Invalidate and refetch
      queryClient.invalidateQueries({
        queryKey: ["instance-preferences", instanceId],
      })
    },
  })

  return {
    preferences,
    isLoading,
    error,
    updatePreferences: updateMutation.mutate,
    isUpdating: updateMutation.isPending,
  }
}