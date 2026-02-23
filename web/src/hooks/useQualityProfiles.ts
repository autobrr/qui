/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { QualityProfile, QualityProfileInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

export const QUALITY_PROFILES_QUERY_KEY = ["quality-profiles"]

export function useQualityProfiles() {
  return useQuery<QualityProfile[]>({
    queryKey: QUALITY_PROFILES_QUERY_KEY,
    queryFn: () => api.listQualityProfiles(),
    staleTime: 30000,
    gcTime: 300000,
  })
}

export function useCreateQualityProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: QualityProfileInput) => api.createQualityProfile(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUALITY_PROFILES_QUERY_KEY })
      toast.success("Quality profile created")
    },
    onError: (error: unknown) => {
      const msg = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to create quality profile: ${msg}`)
    },
  })
}

export function useUpdateQualityProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: QualityProfileInput }) =>
      api.updateQualityProfile(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUALITY_PROFILES_QUERY_KEY })
      toast.success("Quality profile updated")
    },
    onError: (error: unknown) => {
      const msg = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to update quality profile: ${msg}`)
    },
  })
}

export function useDeleteQualityProfile() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.deleteQualityProfile(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUALITY_PROFILES_QUERY_KEY })
      toast.success("Quality profile deleted")
    },
    onError: (error: unknown) => {
      const msg = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to delete quality profile: ${msg}`)
    },
  })
}
