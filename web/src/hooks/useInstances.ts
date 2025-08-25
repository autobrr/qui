/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { InstanceResponse } from "@/types"

export function useInstances() {
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    refetchInterval: 30000, // Refetch every 30 seconds for a single-user app
  })

  const createMutation = useMutation({
    mutationFn: (data: {
      name: string
      host: string
      username: string
      password: string
      basicUsername?: string
      basicPassword?: string
    }) => api.createInstance(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { 
      id: number
      data: Partial<{
        name: string
        host: string
        username: string
        password: string
        basicUsername?: string
        basicPassword?: string
      }>
    }) => api.updateInstance(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: ({ id }: { id: number; name: string }) => api.deleteInstance(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const testConnectionMutation = useMutation({
    mutationFn: (id: number) => api.testConnection(id),
  })

  return {
    instances: instances as InstanceResponse[] | undefined,
    isLoading,
    error,
    createInstance: createMutation.mutate,
    updateInstance: updateMutation.mutate,
    deleteInstance: deleteMutation.mutate,
    testConnection: testConnectionMutation.mutateAsync,
    isCreating: createMutation.isPending,
    isUpdating: updateMutation.isPending,
    isDeleting: deleteMutation.isPending,
    isTesting: testConnectionMutation.isPending,
  }
}