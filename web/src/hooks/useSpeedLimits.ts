/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { SpeedLimitsStatus, SetSpeedLimitsRequest } from "@/types"

export function useSpeedLimitsStatus(instanceId: number, options?: {
  enabled?: boolean
  refetchInterval?: number
}) {
  return useQuery({
    queryKey: ["speed-limits-status", instanceId],
    queryFn: () => api.getSpeedLimitsStatus(instanceId),
    enabled: options?.enabled ?? true,
    refetchInterval: options?.refetchInterval ?? 30000, // Refresh every 30 seconds
    staleTime: 15000,
    gcTime: 300000,
    retry: 1,
  })
}

export function useToggleSpeedLimits(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.toggleSpeedLimits(instanceId),
    onMutate: async () => {
      // Cancel any outgoing refetches
      await queryClient.cancelQueries({ queryKey: ["speed-limits-status", instanceId] })

      // Snapshot the previous value for rollback
      const previousStatus = queryClient.getQueryData<SpeedLimitsStatus>(["speed-limits-status", instanceId])

      // Optimistically update the status
      if (previousStatus) {
        queryClient.setQueryData<SpeedLimitsStatus>(["speed-limits-status", instanceId], {
          ...previousStatus,
          alternativeSpeedLimitsEnabled: !previousStatus.alternativeSpeedLimitsEnabled,
        })
      }

      return { previousStatus }
    },
    onError: (_error, _variables, context) => {
      // Rollback on error
      if (context?.previousStatus) {
        queryClient.setQueryData(["speed-limits-status", instanceId], context.previousStatus)
      }
    },
    onSuccess: () => {
      // Invalidate and refetch after successful toggle
      queryClient.invalidateQueries({ queryKey: ["speed-limits-status", instanceId] })
      
      // Also invalidate server state queries since they include speed limit info
      queryClient.invalidateQueries({ queryKey: ["server-state", instanceId] })
    },
  })
}

export function useSetSpeedLimits(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: SetSpeedLimitsRequest) => api.setSpeedLimits(instanceId, request),
    onSuccess: () => {
      // Invalidate and refetch speed limits status
      queryClient.invalidateQueries({ queryKey: ["speed-limits-status", instanceId] })
      
      // Also invalidate server state queries since they include speed limit info
      queryClient.invalidateQueries({ queryKey: ["server-state", instanceId] })
    },
  })
}