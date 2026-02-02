/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { AppPreferences } from "@/types"

type UseInstancePreferencesOptions = {
  enabled?: boolean
}

export function useInstancePreferences(instanceId: number | undefined, options: UseInstancePreferencesOptions = {}) {
  const queryClient = useQueryClient()
  const shouldEnable = options.enabled ?? true
  const queryEnabled = shouldEnable && typeof instanceId === "number"
  const queryKey = ["instance-preferences", instanceId] as const

  const { data: preferences, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => api.getInstancePreferences(instanceId!),
    enabled: queryEnabled,
    staleTime: 5000, // 5 seconds
    refetchInterval: 60000, // Refetch every minute
    placeholderData: (previousData) => previousData,
  })

  const updateMutation = useMutation({
    mutationFn: (updatedPreferences: Partial<AppPreferences>) => {
      if (instanceId === undefined) throw new Error("No instance ID")
      return api.updateInstancePreferences(instanceId, updatedPreferences)
    },
    onMutate: async (newPreferences) => {
      if (instanceId === undefined) {
        return { previousPreferences: undefined }
      }
      // Cancel outgoing refetches
      await queryClient.cancelQueries({
        queryKey,
      })

      // Snapshot previous value
      const previousPreferences = queryClient.getQueryData<AppPreferences>(queryKey)

      // Optimistically update
      if (previousPreferences) {
        queryClient.setQueryData(
          queryKey,
          { ...previousPreferences, ...newPreferences }
        )
      }

      return { previousPreferences }
    },
    onError: (_err, _newPreferences, context) => {
      // Rollback on error
      if (context?.previousPreferences) {
        queryClient.setQueryData(
          queryKey,
          context.previousPreferences
        )
      }
    },
    onSuccess: () => {
      // Invalidate and refetch
      queryClient.invalidateQueries({
        queryKey,
      })
    },
  })

  type UpdatePreferencesOptions = Parameters<typeof updateMutation.mutate>[1]

  return {
    preferences,
    isLoading,
    error,
    updatePreferences: (updatedPreferences: Partial<AppPreferences>, options?: UpdatePreferencesOptions) =>
      updateMutation.mutate(updatedPreferences, options),
    isUpdating: updateMutation.isPending,
  }
}
