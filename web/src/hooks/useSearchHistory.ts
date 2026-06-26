/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"

interface UseSearchHistoryOptions {
  limit?: number
  enabled?: boolean
  refetchInterval?: number | false
}

export const useSearchHistory = (options: UseSearchHistoryOptions = {}) => {
  const { limit = 50, enabled = true, refetchInterval = false } = options

  return useQuery({
    queryKey: ["searchHistory", limit],
    queryFn: () => api.getSearchHistory(limit),
    enabled,
    refetchInterval,
    staleTime: 5 * 1000, // 5 seconds
    refetchOnWindowFocus: false,
  })
}
