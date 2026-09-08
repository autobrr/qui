/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

/**
 * Observes the cached auth state without fetching (enabled: false); useAuth
 * owns the fetch. Sync hooks gate their auth-only work on this so nothing
 * runs on the login/setup pages.
 */
export function useIsAuthed(): boolean {
  const { data: user } = useQuery({
    queryKey: ["auth", "user"],
    queryFn: () => api.checkAuth(),
    enabled: false,
  })
  return Boolean(user)
}
