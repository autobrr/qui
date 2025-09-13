/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { User } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

export function useAuth() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: user, isLoading, error } = useQuery({
    queryKey: ["auth", "user"],
    queryFn: () => api.checkAuth(),
    retry: false,
    staleTime: Infinity,
  })

  const loginMutation = useMutation({
    mutationFn: ({ username, password, rememberMe = false }: { username: string; password: string; rememberMe?: boolean }) =>
      api.login(username, password, rememberMe),
    onSuccess: async (data) => {
      // Set user data immediately
      queryClient.setQueryData(["auth", "user"], data.user)

      // Session warming: Prefetch critical data before navigation
      // This runs in parallel and doesn't block navigation
      const prefetchPromises = [
        // Prefetch instances list
        queryClient.prefetchQuery({
          queryKey: ["instances"],
          queryFn: api.getInstances,
          staleTime: 5 * 60 * 1000, // 5 minutes
        }),
        // Prefetch licensed themes if applicable
        queryClient.prefetchQuery({
          queryKey: ["themes", "licensed"],
          queryFn: api.getLicensedThemes,
          staleTime: 10 * 60 * 1000, // 10 minutes
        }),
      ]

      // Also prefetch torrents for the first instance if available
      api.getInstances().then(instances => {
        if (instances && instances.length > 0) {
          const firstInstance = instances[0]
          // Prefetch first page of torrents for the default instance
          queryClient.prefetchQuery({
            queryKey: ["torrents-list", firstInstance.id, 0, undefined, "", "added_on", "desc"],
            queryFn: () => api.getTorrents(firstInstance.id, {
              page: 0,
              limit: 300,
              sort: "added_on",
              order: "desc",
            }),
            staleTime: 0, // Let backend handle caching
          })
        }
      }).catch(error => {
        console.warn("Failed to prefetch torrents:", error)
      })

      // Start prefetching but don't await - navigate immediately
      Promise.all(prefetchPromises).catch(error => {
        console.warn("Failed to prefetch some data:", error)
      })

      navigate({ to: "/dashboard" })
    },
  })

  const setupMutation = useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      api.setup(username, password),
    onSuccess: (data) => {
      // Set user data immediately
      queryClient.setQueryData(["auth", "user"], data.user)

      navigate({ to: "/dashboard" })
    },
  })

  const logoutMutation = useMutation({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      queryClient.setQueryData(["auth", "user"], null)
      queryClient.clear()
      navigate({ to: "/login" })
    },
  })

  return {
    user: user as User | undefined,
    isAuthenticated: !!user,
    isLoading,
    error,
    login: loginMutation.mutate,
    setup: setupMutation.mutate,
    logout: logoutMutation.mutate,
    isLoggingIn: loginMutation.isPending,
    isSettingUp: setupMutation.isPending,
    loginError: loginMutation.error,
    setupError: setupMutation.error,
  }
}