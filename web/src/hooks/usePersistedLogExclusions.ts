/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { LogExclusions } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback } from "react"

const QUERY_KEY = ["log-exclusions"]

export function usePersistedLogExclusions() {
  const queryClient = useQueryClient()

  const { data } = useQuery<LogExclusions>({
    queryKey: QUERY_KEY,
    queryFn: () => api.getLogExclusions(),
    staleTime: 60000,
    gcTime: 300000,
  })

  const mutation = useMutation({
    mutationFn: (patterns: string[]) => api.updateLogExclusions({ patterns }),
    onMutate: async (newPatterns) => {
      await queryClient.cancelQueries({ queryKey: QUERY_KEY })
      const previous = queryClient.getQueryData<LogExclusions>(QUERY_KEY)

      if (previous) {
        queryClient.setQueryData<LogExclusions>(QUERY_KEY, {
          ...previous,
          patterns: newPatterns,
        })
      }

      return { previous }
    },
    onError: (_err, _newPatterns, context) => {
      if (context?.previous) {
        queryClient.setQueryData(QUERY_KEY, context.previous)
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
  })

  const setExclusions = useCallback(
    (patterns: string[]) => {
      mutation.mutate(patterns)
    },
    [mutation.mutate]
  )

  const exclusions = data?.patterns ?? []

  return [exclusions, setExclusions] as const
}
