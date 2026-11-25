/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"

export function useIsAuthed(): boolean {
  const { data: user } = useQuery({
    queryKey: ["auth", "user"],
    queryFn: () => api.checkAuth(),
    enabled: false,
  })
  return Boolean(user)
}
