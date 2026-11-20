/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { navigateAfterAuth } from "@/lib/add-intent"
import { api } from "@/lib/api"
import type { User } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"

/**
 * Set once the first account exists, so Setup's route guard does not navigate
 * over the post-setup destination. Session storage survives the redirect but
 * not a browser restart, which is where the marker should end.
 */
export const SETUP_COMPLETED_KEY = "qui-setup-completed"

export function markSetupCompleted(): void {
  try {
    sessionStorage.setItem(SETUP_COMPLETED_KEY, "1")
  } catch {
    // Storage can be unavailable in some embedded browsers; the post-setup navigation still runs.
  }
}

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
    onSuccess: (data) => {
      queryClient.setQueryData(["auth", "user"], data.user)
      // Refetch the theme catalog now that the session can unlock the full
      // premium CSS: the pre-login fetch only carried the selected theme.
      queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
      navigateAfterAuth(navigate)
    },
  })

  const setupMutation = useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      api.setup(username, password),
    onSuccess: (data) => {
      queryClient.setQueryData(["auth", "user"], data.user)
      // Both entry points cache `setup-required` for the process lifetime, so
      // a later visit to either route must see that the first account exists.
      queryClient.setQueryData(["setup-required"], false)
      queryClient.setQueryData(["setup-required-page"], false)
      queryClient.invalidateQueries({ queryKey: ["setup-required"] })
      queryClient.invalidateQueries({ queryKey: ["setup-required-page"] })
      queryClient.invalidateQueries({ queryKey: ["builtin-themes"] })
      markSetupCompleted()
      navigateAfterAuth(navigate)
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

  const setIsAuthenticated = (authenticated: boolean) => {
    if (authenticated) {
      // Force refetch of user data
      queryClient.invalidateQueries({ queryKey: ["auth", "user"] })
    } else {
      queryClient.setQueryData(["auth", "user"], null)
    }
  }

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
    setIsAuthenticated,
  }
}
