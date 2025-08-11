/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import type { InstanceResponse } from '@/types'

export function useInstances() {
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error } = useQuery({
    queryKey: ['instances'],
    queryFn: () => api.getInstances(),
    refetchInterval: 30000, // Refetch every 30 seconds for a single-user app
  })

  const createMutation = useMutation({
    mutationFn: (data: {
      name: string
      host: string
      port: number
      username: string
      password: string
    }) => api.createInstance(data),
    onSuccess: (data) => {
      if (data.connected) {
        toast.success('Instance Created', {
          description: 'Instance created and connected successfully'
        })
      } else {
        toast.warning('Instance Created with Connection Issue', {
          description: data.connectionError || 'Instance created but could not connect'
        })
      }
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
    onError: (error: Error) => {
      toast.error('Create Failed', {
        description: error.message || 'Failed to create instance'
      })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { 
      id: number
      data: Partial<{
        name: string
        host: string
        port: number
        username: string
        password: string
      }>
    }) => api.updateInstance(id, data),
    onSuccess: (data) => {
      if (data.connected) {
        toast.success('Instance Updated', {
          description: 'Instance updated and connected successfully'
        })
      } else {
        toast.warning('Instance Updated with Connection Issue', {
          description: data.connectionError || 'Instance updated but could not connect'
        })
      }
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
    onError: (error: Error) => {
      toast.error('Update Failed', {
        description: error.message || 'Failed to update instance'
      })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: ({ id }: { id: number; name: string }) => api.deleteInstance(id),
    onSuccess: (_, variables) => {
      toast.success('Instance Deleted', {
        description: `Successfully deleted "${variables.name}"`
      })
      queryClient.invalidateQueries({ queryKey: ['instances'] })
    },
    onError: (error: Error) => {
      toast.error('Delete Failed', {
        description: error.message || 'Failed to delete instance'
      })
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