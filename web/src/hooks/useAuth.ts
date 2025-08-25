/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient, } from "@tanstack/react-query"
import { useNavigate, } from "@tanstack/react-router"
import { api, } from "@/lib/api"
import type { User, } from "@/types"

export function useAuth() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: user, isLoading, error, } = useQuery({
    queryKey: ["auth", "user",],
    queryFn: () => api.checkAuth(),
    retry: false,
    staleTime: Infinity,
  },)

  const loginMutation = useMutation({
    mutationFn: ({ username, password, }: { username: string; password: string },) =>
      api.login(username, password,),
    onSuccess: (data,) => {
      queryClient.setQueryData(["auth", "user",], data.user,)
      navigate({ to: "/dashboard", },)
    },
  },)

  const setupMutation = useMutation({
    mutationFn: ({ username, password, }: { username: string; password: string },) =>
      api.setup(username, password,),
    onSuccess: (data,) => {
      queryClient.setQueryData(["auth", "user",], data.user,)
      navigate({ to: "/dashboard", },)
    },
  },)

  const logoutMutation = useMutation({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      queryClient.setQueryData(["auth", "user",], null,)
      queryClient.clear()
      navigate({ to: "/login", },)
    },
  },)

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