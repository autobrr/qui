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
  // Companion cache entry holding the X-Qui-Cached-At staleness timestamp for the
  // fetched preferences. Kept separate so the preferences query data stays a plain
  // AppPreferences (other code reads that cache key directly).
  const cachedAtQueryKey = useMemo(
    () => ["instance-preferences-cached-at", instanceId] as const,
    [instanceId]
  )

  const cachedMetadata = queryClient.getQueryData<InstanceMetadata | undefined>(metadataQueryKey)
  const cachedPreferences =
    queryClient.getQueryData<AppPreferences | undefined>(preferencesQueryKey) ??
    cachedMetadata?.preferences

  const queryEnabled =
    Boolean(externalEnabled) && fetchIfMissing && typeof instanceId === "number" && !cachedPreferences

  const { data: preferences, isLoading, error, refetch } = useQuery<AppPreferences | undefined>({
    queryKey: preferencesQueryKey,
    queryFn: async () => {
      if (instanceId === undefined) {
        return undefined
      }

      // Always hit the backend so an explicit refetch (e.g. when the dialog opens)
      // re-checks qBittorrent and re-derives staleness. Cached metadata only seeds the
      // initial render via initialData below; short-circuiting here would return stale
      // data with a cleared cachedAt even while qBittorrent is still unreachable.
      const { preferences: fresh, cachedAt } = await api.getInstancePreferencesWithMeta(instanceId)
      queryClient.setQueryData<Date | null>(cachedAtQueryKey, cachedAt)
      // Only enrich an existing metadata entry with the fetched preferences. Creating
      // one here would seed empty categories/tags, which makes useInstanceMetadata
      // treat metadata as complete and skip its categories/tags fallback, leaving
      // those selectors permanently empty. The preferences themselves live in the
      // instance-preferences cache (this query's own key).
      queryClient.setQueryData<InstanceMetadata | undefined>(metadataQueryKey, previous =>
        previous ? { ...previous, preferences: fresh } : previous
      )

      return fresh
    },
    enabled: queryEnabled,
    staleTime: cachedPreferences ? Infinity : 60000,
    gcTime: 1800000,
    refetchInterval: false,
    placeholderData: previousData => previousData,
    initialData: () => cachedPreferences,
  })

  // Cache-only subscription so the dialog re-renders when the preferences queryFn
  // records (or clears) the staleness timestamp. No queryFn: the value is written
  // exclusively by the preferences query above.
  const { data: cachedAt = null } = useQuery<Date | null>({
    queryKey: cachedAtQueryKey,
    enabled: false,
    initialData: null,
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
          queryClient.setQueryData<InstanceMetadata | undefined>(
            metadataQueryKey,
            previous => (previous ? { ...previous, preferences: optimistic } : previous)
          )
        }
      }

      return { previousPreferences, previousMetadata }
    },
    onError: (_err, _newPreferences, context) => {
      const rollbackPreferences =
        context?.previousPreferences ?? context?.previousMetadata?.preferences

      if (rollbackPreferences) {
        queryClient.setQueryData(preferencesQueryKey, rollbackPreferences)
      }

      if (context?.previousMetadata) {
        queryClient.setQueryData(metadataQueryKey, context.previousMetadata)
      }
    },
    onSuccess: (updatedPreferences) => {
      queryClient.setQueryData(preferencesQueryKey, updatedPreferences)
      // A successful write means qBittorrent is reachable again, so the displayed
      // settings are now fresh: clear any stale "showing cached settings" marker.
      queryClient.setQueryData<Date | null>(cachedAtQueryKey, null)
      // Same rule as the fetch path: only merge into an existing metadata entry, never
      // fabricate one with empty categories/tags.
      queryClient.setQueryData<InstanceMetadata | undefined>(metadataQueryKey, previous =>
        previous ? { ...previous, preferences: updatedPreferences } : previous
      )
    },
  })

  type UpdatePreferencesOptions = Parameters<typeof updateMutation.mutate>[1]

  return {
    preferences: resolvedPreferences,
    isLoading: fetchIfMissing && externalEnabled ? (isLoading && !resolvedPreferences) : false,
    error,
    // Non-null only when the backend served cached preferences after a live
    // qBittorrent call failed (X-Qui-Cached-At header present).
    cachedAt,
    // Force a fresh preferences fetch (e.g. when the dialog opens) so the staleness
    // marker reflects current qBittorrent reachability rather than a one-time load.
    refetch,
    updatePreferences: (updatedPreferences: Partial<AppPreferences>, options?: UpdatePreferencesOptions) =>
      updateMutation.mutate(updatedPreferences, options),
    isUpdating: updateMutation.isPending,
  }
}
