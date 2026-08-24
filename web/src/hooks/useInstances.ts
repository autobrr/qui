/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { InstanceFormData, InstanceResponse } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

export function useInstances() {
  const queryClient = useQueryClient()

  const { data: instances, isLoading, error } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    // Instance list rarely changes; real-time connection status now comes from SSE
    refetchInterval: 2 * 60 * 1000, // Refetch every 2 minutes
  })

  const createMutation = useMutation({
    mutationFn: (data: InstanceFormData) => api.createInstance(data),
    onSuccess: async (newInstance) => {
      // Immediately add the new instance to cache
      queryClient.setQueryData<InstanceResponse[]>(["instances"], (old) => {
        if (!old) return [newInstance]
        return [...old.filter(i => i.id !== newInstance.id), newInstance]
      })

      // Test connection immediately to get actual status
      try {
        const status = await api.testConnection(newInstance.id)
        // Update the instance with the actual connection status
        queryClient.setQueryData<InstanceResponse[]>(["instances"], (old) => {
          if (!old) return []
          return old.map(i =>
            i.id === newInstance.id? { ...i, connected: status.connected }: i
          )
        })
      } catch (error) {
        console.error("Failed to test connection after creation:", error)
      }

      // Invalidate to ensure consistency with backend
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: {
      id: number
      data: Partial<InstanceFormData>
    }) => api.updateInstance(id, data),
    onSuccess: async (updatedInstance) => {
      // Immediately update the instance in cache, preserving only the connected
      // flag to avoid UI flicker (testConnection will refresh it)
      queryClient.setQueryData<InstanceResponse[]>(["instances"], (old) => {
        if (!old) return [updatedInstance]
        return old.map(i => i.id === updatedInstance.id? { ...updatedInstance, connected: i.connected }: i)
      })

      // Test connection immediately to get actual status
      try {
        const status = await api.testConnection(updatedInstance.id)
        // Update the instance with the actual connection status
        queryClient.setQueryData<InstanceResponse[]>(["instances"], (old) => {
          if (!old) return []
          return old.map(i =>
            i.id === updatedInstance.id? { ...i, connected: status.connected }: i
          )
        })
      } catch (error) {
        console.error("Failed to test connection after update:", error)
      }

      // Invalidate to ensure consistency with backend
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const statusMutation = useMutation<InstanceResponse, Error, { id: number; isActive: boolean }, { previousInstances?: InstanceResponse[] }>({
    mutationFn: ({ id, isActive }) => api.updateInstanceStatus(id, isActive),
    onMutate: async ({ id, isActive }) => {
      await queryClient.cancelQueries({ queryKey: ["instances"] })
      const previousInstances = queryClient.getQueryData<InstanceResponse[]>(["instances"])

      if (previousInstances) {
        queryClient.setQueryData<InstanceResponse[]>(["instances"], previousInstances.map(instance =>
          instance.id === id? {
            ...instance,
            isActive,
            connected: false,
            connectionStatus: isActive ? "" : "disabled",
          }: instance
        ))
      }

      return { previousInstances }
    },
    onError: (_error, _variables, context) => {
      if (context?.previousInstances) {
        queryClient.setQueryData(["instances"], context.previousInstances)
      }
    },
    onSuccess: (updatedInstance) => {
      queryClient.setQueryData<InstanceResponse[]>(["instances"], (instances) => {
        if (!instances) return [updatedInstance]
        return instances.map(instance => instance.id === updatedInstance.id ? updatedInstance : instance)
      })
    },
    onSettled: () => {
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

  const reorderMutation = useMutation<InstanceResponse[], Error, number[], { previousInstances?: InstanceResponse[] }>({
    mutationFn: (instanceIds: number[]) => api.reorderInstances(instanceIds),
    onMutate: async (instanceIds) => {
      await queryClient.cancelQueries({ queryKey: ["instances"] })

      const previousInstances = queryClient.getQueryData<InstanceResponse[]>(["instances"])
      if (previousInstances) {
        const instanceMap = new Map(previousInstances.map(instance => [instance.id, instance]))
        const reorderedInstances = instanceIds
          .map((id, index) => {
            const instance = instanceMap.get(id)
            if (!instance) return null
            return { ...instance, sortOrder: index }
          })
          .filter((instance): instance is InstanceResponse => instance !== null)

        if (reorderedInstances.length === previousInstances.length) {
          queryClient.setQueryData<InstanceResponse[]>(["instances"], reorderedInstances)
        }
      }

      return { previousInstances }
    },
    onError: (_error, _instanceIds, context) => {
      if (context?.previousInstances) {
        queryClient.setQueryData(["instances"], context.previousInstances)
      }
    },
    onSuccess: (data) => {
      queryClient.setQueryData<InstanceResponse[]>(["instances"], data)
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ["instances"] })
    },
  })

  const updatingStatusId = statusMutation.isPending? statusMutation.variables?.id ?? null: null

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
    reorderInstances: reorderMutation.mutate,
    reorderInstancesAsync: reorderMutation.mutateAsync,
    isReordering: reorderMutation.isPending,
    setInstanceStatus: statusMutation.mutate,
    isUpdatingStatus: statusMutation.isPending,
    updatingStatusId,
  }
}
