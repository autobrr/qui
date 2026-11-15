/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { AppPreferences, PreferenceField } from "@/types"

interface UseInstancePreferencesOptions {
  enabled?: boolean
}

export const valuesOf = (fields: PreferenceField[]) => Object.fromEntries(fields.map(field => [field.key, field.value]))

// The view holds only the keys that the catalog shows. usePreferenceValues.test.tsx fails when a
// screen reads a key outside the view.
const preferencesOf = (fields: PreferenceField[]) => valuesOf(fields) as unknown as AppPreferences

// Reads the preference values from the preferences view. The view is fetched when a screen opens
// and set again from the view that a save returns.
export function useInstancePreferences(
  instanceId: number | undefined,
  options: UseInstancePreferencesOptions = {}
) {
  const { enabled = true } = options
  const queryClient = useQueryClient()
  const queryKey = useMemo(
    () => ["instance-preferences", instanceId] as const,
    [instanceId]
  )

  const { data: preferences } = useQuery({
    queryKey,
    queryFn: async () => preferencesOf(await api.getInstancePreferenceView(instanceId!)),
    // Instance 0 is the all-instances view, which has no preferences.
    enabled: enabled && instanceId !== undefined && instanceId > 0,
    gcTime: 1800000,
  })

  const updateMutation = useMutation<
    AppPreferences,
    Error,
    Partial<AppPreferences>,
    { previousPreferences?: AppPreferences }
  >({
    mutationFn: async (partialPreferences: Partial<AppPreferences>) => {
      if (instanceId === undefined) throw new Error("No instance ID")
      return preferencesOf(await api.updateInstancePreferences(instanceId, partialPreferences))
    },
    onMutate: async (newPreferences) => {
      await queryClient.cancelQueries({ queryKey })

      const previousPreferences = queryClient.getQueryData<AppPreferences>(queryKey)
      if (previousPreferences) {
        queryClient.setQueryData(queryKey, { ...previousPreferences, ...newPreferences })
      }

      return { previousPreferences }
    },
    onError: (_err, _newPreferences, context) => {
      if (context?.previousPreferences) {
        queryClient.setQueryData(queryKey, context.previousPreferences)
      }
    },
    onSuccess: (updatedPreferences) => {
      queryClient.setQueryData(queryKey, updatedPreferences)
    },
  })

  return {
    preferences,
    updatePreferences: updateMutation.mutateAsync,
    isUpdating: updateMutation.isPending,
  }
}
