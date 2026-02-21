/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import type { AppPreferences } from "@/types"

interface UseInstancePreferencesOptions {
  fetchIfMissing?: boolean
  enabled?: boolean
}

export function useInstancePreferences(
  instanceId: number | undefined,
  options: UseInstancePreferencesOptions = {}
) {
  const { fetchIfMissing = true, enabled: externalEnabled = true } = options
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

  const queryEnabled =
    Boolean(externalEnabled) && fetchIfMissing && typeof instanceId === "number" && !cachedPreferences

  const { data: preferences, isLoading, error } = useQuery<AppPreferences | undefined>({
    queryKey: preferencesQueryKey,
    queryFn: async () => {
      if (instanceId === undefined) {
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
    initialData: () => cachedPreferences,
  })

  const resolvedPreferences = preferences ?? cachedPreferences

  const updateMutation = useMutation<
    AppPreferences,
    Error,
    Partial<AppPreferences>,
    { previousPreferences?: AppPreferences; previousMetadata?: InstanceMetadata }
  >({
    mutationFn: (partialPreferences: Partial<AppPreferences>) => {
      if (instanceId === undefined) throw new Error("No instance ID")
      return api.updateInstancePreferences(instanceId, partialPreferences)
    },
    onMutate: async (newPreferences) => {
      if (instanceId === undefined) {
        return { previousPreferences: undefined, previousMetadata: undefined }
      }

      await queryClient.cancelQueries({
        queryKey: preferencesQueryKey,
      })

      const previousPreferences = queryClient.getQueryData<AppPreferences | undefined>(
        preferencesQueryKey
      )
      const previousMetadata = queryClient.getQueryData<InstanceMetadata | undefined>(
        metadataQueryKey
      )

      const basePreferences =
        previousPreferences ?? previousMetadata?.preferences

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

  type UpdatePreferencesOptions = Parameters<typeof updateMutation.mutate>[1]

  return {
    preferences: resolvedPreferences,
    isLoading: fetchIfMissing && externalEnabled ? (isLoading && !resolvedPreferences) : false,
    error,
    updatePreferences: (updatedPreferences: Partial<AppPreferences>, options?: UpdatePreferencesOptions) =>
      updateMutation.mutate(updatedPreferences, options),
    isUpdating: updateMutation.isPending,
  }
}
