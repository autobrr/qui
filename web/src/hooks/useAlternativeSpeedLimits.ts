/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"

export function useAlternativeSpeedLimits(instanceId: number | undefined) {
  const queryClient = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ["alternative-speed-limits", instanceId],
    queryFn: () => instanceId ? api.getAlternativeSpeedLimitsMode(instanceId) : null,
    enabled: !!instanceId,
    staleTime: 5000, // 5 seconds
    refetchInterval: 30000, // Refetch every 30 seconds
    placeholderData: (previousData) => previousData,
  })

  const toggleMutation = useMutation({
    mutationFn: () => {
      if (!instanceId) throw new Error("No instance ID")
      return api.toggleAlternativeSpeedLimits(instanceId)
    },
    onMutate: async () => {
      // Cancel outgoing refetches
      await queryClient.cancelQueries({
        queryKey: ["alternative-speed-limits", instanceId],
      })

      // Snapshot previous value
      const previousData = queryClient.getQueryData<{ enabled: boolean }>(
        ["alternative-speed-limits", instanceId]
      )

      // Optimistically update
      if (previousData) {
        queryClient.setQueryData(
          ["alternative-speed-limits", instanceId],
          { enabled: !previousData.enabled }
        )
      }

      return { previousData }
    },
    onError: (_err, _variables, context) => {
      // Rollback on error
      if (context?.previousData) {
        queryClient.setQueryData(
          ["alternative-speed-limits", instanceId],
          context.previousData
        )
      }
    },
    onSuccess: () => {
      // Invalidate and refetch
      queryClient.invalidateQueries({
        queryKey: ["alternative-speed-limits", instanceId],
      })
    },
  })

  return {
    enabled: data?.enabled ?? false,
    isLoading,
    error,
    toggle: toggleMutation.mutate,
    isToggling: toggleMutation.isPending,
  }
}