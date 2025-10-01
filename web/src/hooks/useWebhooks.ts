/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { WebhookPreferences } from "@/types"

export function useWebhooks() {
  const queryClient = useQueryClient()

  const { data: webhooks, isLoading, error } = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api.getAllWebhooks(),
    refetchInterval: 30000, // Refetch every 30 seconds
  })

  const updateMutation = useMutation({
    mutationFn: ({ instanceId, preferences }: {
      instanceId: number
      preferences: Partial<WebhookPreferences>
    }) => api.updateWebhookPreferences(instanceId, preferences),
    onMutate: async ({ instanceId, preferences }) => {
      // Cancel outgoing refetches
      await queryClient.cancelQueries({
        queryKey: ["webhooks"],
      })

      // Snapshot previous value
      const previousWebhooks = queryClient.getQueryData<WebhookPreferences[]>(
        ["webhooks"]
      )

      // Optimistically update
      if (previousWebhooks) {
        queryClient.setQueryData<WebhookPreferences[]>(
          ["webhooks"],
          previousWebhooks.map(w =>
            w.instance_id === instanceId.toString()
              ? { ...w, ...preferences }
              : w
          )
        )
      }

      return { previousWebhooks }
    },
    onError: (_err, _variables, context) => {
      // Rollback on error
      if (context?.previousWebhooks) {
        queryClient.setQueryData(
          ["webhooks"],
          context.previousWebhooks
        )
      }
    },
    onSuccess: () => {
      // Invalidate and refetch
      queryClient.invalidateQueries({
        queryKey: ["webhooks"],
      })
    },
  })

  const createApiKeyMutation = useMutation({
    mutationFn: ({ instanceId, instanceName }: {
      instanceId: number
      instanceName: string
    }) => api.createClientApiKey({
      clientName: `Webhook - ${instanceName}`,
      instanceId,
      isWebhook: true,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["webhooks"],
      })
    },
  })

  const deleteApiKeyMutation = useMutation({
    mutationFn: (apiKeyId: number) => api.deleteClientApiKey(apiKeyId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["webhooks"],
      })
    },
  })

  return {
    webhooks: webhooks as WebhookPreferences[] | undefined,
    isLoading,
    error,
    updateWebhook: updateMutation.mutate,
    updateWebhookAsync: updateMutation.mutateAsync,
    isUpdating: updateMutation.isPending,
    createApiKey: createApiKeyMutation.mutate,
    createApiKeyAsync: createApiKeyMutation.mutateAsync,
    isCreatingApiKey: createApiKeyMutation.isPending,
    deleteApiKey: deleteApiKeyMutation.mutate,
    deleteApiKeyAsync: deleteApiKeyMutation.mutateAsync,
    isDeletingApiKey: deleteApiKeyMutation.isPending,
  }
}

