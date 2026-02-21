/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { TorrentStreamPayload } from "@/types"
import { useCallback, useEffect, useMemo, useState } from "react"

export function useAlternativeSpeedLimits(instanceId: number | undefined) {
  const queryClient = useQueryClient()
  const [streamEnabled, setStreamEnabled] = useState<boolean | undefined>(undefined)

  const streamParams = useMemo(() => {
    if (!instanceId) {
      return null
    }

    return {
      instanceId,
      page: 0,
      limit: 1,
      sort: "added_on",
      order: "desc" as const,
    }
  }, [instanceId])

  const handleStreamMessage = useCallback((payload: TorrentStreamPayload) => {
    if (!instanceId) {
      return
    }

    const value = payload.data?.serverState?.use_alt_speed_limits
    if (typeof value !== "boolean") {
      return
    }

    setStreamEnabled(value)
    queryClient.setQueryData(["alternative-speed-limits", instanceId], { enabled: value })
  }, [instanceId, queryClient])

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams),
    onMessage: handleStreamMessage,
  })

  useEffect(() => {
    setStreamEnabled(undefined)
  }, [instanceId])

  const shouldUseFallbackQuery = Boolean(instanceId) && (!streamState.connected || !!streamState.error)

  const { data, isLoading: isFallbackLoading, error } = useQuery({
    queryKey: ["alternative-speed-limits", instanceId],
    queryFn: () => instanceId ? api.getAlternativeSpeedLimitsMode(instanceId) : null,
    enabled: shouldUseFallbackQuery,
    staleTime: 5000, // 5 seconds
    refetchInterval: shouldUseFallbackQuery ? 30000 : false,
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
    enabled: streamEnabled ?? data?.enabled ?? false,
    isLoading: streamEnabled === undefined && isFallbackLoading,
    error,
    toggle: toggleMutation.mutate,
    isToggling: toggleMutation.isPending,
  }
}
