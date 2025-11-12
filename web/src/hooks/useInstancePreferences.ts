/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { AppPreferences } from "@/types"

type UseInstancePreferencesOptions = {
  enabled?: boolean
}

export function useInstancePreferences(instanceId: number | undefined, options: UseInstancePreferencesOptions = {}) {
  const queryClient = useQueryClient()
  const shouldEnable = options.enabled ?? true
  const effectiveInstanceId = shouldEnable ? instanceId : undefined
  const queryKey = ["instance-preferences", effectiveInstanceId] as const

  const { data: preferences, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => effectiveInstanceId ? api.getInstancePreferences(effectiveInstanceId) : null,
    enabled: !!effectiveInstanceId,
    staleTime: 5000, // 5 seconds
    refetchInterval: 60000, // Refetch every minute
    placeholderData: (previousData) => previousData,
  })

  const updateMutation = useMutation({
    mutationFn: (updatedPreferences: Partial<AppPreferences>) => {
      if (!effectiveInstanceId) throw new Error("No instance ID")
      return api.updateInstancePreferences(effectiveInstanceId, updatedPreferences)
    },
    onMutate: async (newPreferences) => {
      if (!effectiveInstanceId) {
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

  return {
    preferences,
    isLoading,
    error,
    updatePreferences: updateMutation.mutate,
    isUpdating: updateMutation.isPending,
  }
}
