/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TrackerCustomization, TrackerCustomizationInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

const QUERY_KEY = ["tracker-customizations"]

/**
 * Hook for fetching all tracker customizations (nicknames and merged domains)
 */
export function useTrackerCustomizations() {
  return useQuery<TrackerCustomization[]>({
    queryKey: QUERY_KEY,
    queryFn: () => api.listTrackerCustomizations(),
    staleTime: 30000, // 30 seconds
    gcTime: 300000, // Keep in cache for 5 minutes
  })
}

/**
 * Hook for creating a new tracker customization
 */
export function useCreateTrackerCustomization() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: TrackerCustomizationInput) => api.createTrackerCustomization(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Create failed:", error)
      toast.error("Failed to create tracker customization")
    },
  })
}

/**
 * Hook for updating an existing tracker customization
 */
export function useUpdateTrackerCustomization() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: TrackerCustomizationInput }) =>
      api.updateTrackerCustomization(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Update failed:", error)
      toast.error("Failed to update tracker customization")
    },
  })
}

/**
 * Hook for deleting a tracker customization
 */
export function useDeleteTrackerCustomization() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: number) => api.deleteTrackerCustomization(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Delete failed:", error)
      toast.error("Failed to delete tracker customization")
    },
  })
}
