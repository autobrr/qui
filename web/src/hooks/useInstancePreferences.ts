/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences } from "@/types"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"

interface UseInstancePreferencesOptions {
  fetchIfMissing?: boolean
}

export function useInstancePreferences(
  instanceId: number | undefined,
  options: UseInstancePreferencesOptions = {}
) {
  const { fetchIfMissing = true } = options
  const queryClient = useQueryClient()
  const metadataQueryKey = useMemo(
    () => ["instance-metadata", instanceId] as const,
    [instanceId]
  )
  const preferencesQueryKey = useMemo(
    () => ["instance-preferences", instanceId] as const,
    [instanceId]
  )

  const cachedMetadata = queryClient.getQueryData<InstanceMetadata | undefined>(metadataQueryKey)
  const cachedPreferences =
    queryClient.getQueryData<AppPreferences | undefined>(preferencesQueryKey) ??
    cachedMetadata?.preferences

  const queryEnabled = fetchIfMissing && !!instanceId && !cachedPreferences

  const { data: preferences, isLoading, error } = useQuery<AppPreferences | undefined>({
    queryKey: preferencesQueryKey,
    queryFn: async () => {
      if (!instanceId) {
        return undefined
      }

      if (cachedMetadata?.preferences) {
        return cachedMetadata.preferences
      }

      const fresh = await api.getInstancePreferences(instanceId)
      queryClient.setQueryData(metadataQueryKey, (previous: InstanceMetadata | undefined) => {
        if (!previous) {
          return {
            categories: {},
            tags: [],
            preferences: fresh,
          }
        }

        return {
          ...previous,
          preferences: fresh,
        }
      })

      return fresh
    },
    enabled: queryEnabled,
    staleTime: cachedPreferences ? Infinity : 60000,
    gcTime: 1800000,
    refetchInterval: false,
    placeholderData: previousData => previousData,
    initialData: () => {
      return cachedPreferences
    },
  })

  const resolvedPreferences = preferences ?? cachedPreferences

  const updateMutation = useMutation<
    AppPreferences,
    Error,
    Partial<AppPreferences>,
    { previousPreferences?: AppPreferences; previousMetadata?: InstanceMetadata }
  >({
    mutationFn: (preferences: Partial<AppPreferences>) => {
      if (!instanceId) throw new Error("No instance ID")
      return api.updateInstancePreferences(instanceId, preferences)
    },
    onMutate: async (newPreferences) => {
      // Cancel outgoing refetches
      await queryClient.cancelQueries({
        queryKey: preferencesQueryKey,
      })

      // Snapshot previous value
      const previousPreferences = queryClient.getQueryData<AppPreferences | undefined>(
        preferencesQueryKey
      )
      const previousMetadata = queryClient.getQueryData<InstanceMetadata | undefined>(
        metadataQueryKey
      )

      const basePreferences =
        previousPreferences ?? previousMetadata?.preferences

      // Optimistically update
      if (basePreferences) {
        const optimistic = { ...basePreferences, ...newPreferences }
        queryClient.setQueryData(preferencesQueryKey, optimistic)

        if (previousMetadata) {
          queryClient.setQueryData<InstanceMetadata | undefined>(metadataQueryKey, {
            ...previousMetadata,
            preferences: optimistic,
          })
        }
      }

      return { previousPreferences, previousMetadata }
    },
    onError: (_err, _newPreferences, context) => {
      // Rollback on error
      if (context?.previousPreferences) {
        queryClient.setQueryData(preferencesQueryKey, context.previousPreferences)
      }

      if (context?.previousMetadata) {
        queryClient.setQueryData(metadataQueryKey, context.previousMetadata)
      }
    },
    onSuccess: (updatedPreferences) => {
      queryClient.setQueryData(preferencesQueryKey, updatedPreferences)
      queryClient.setQueryData<InstanceMetadata | undefined>(metadataQueryKey, previous => {
        if (!previous) {
          return {
            categories: {},
            tags: [],
            preferences: updatedPreferences,
          }
        }

        return {
          ...previous,
          preferences: updatedPreferences,
        }
      })
    },
  })

  return {
    preferences: resolvedPreferences,
    isLoading: fetchIfMissing ? (isLoading && !resolvedPreferences) : false,
    error,
    updatePreferences: updateMutation.mutate,
    isUpdating: updateMutation.isPending,
  }
}
