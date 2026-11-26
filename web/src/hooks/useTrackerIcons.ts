/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useActivityStream } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"

export function useTrackerIcons() {
  useActivityStream()

  const query = useQuery<Record<string, string>>({
    queryKey: ["tracker-icons"],
    queryFn: () => api.getTrackerIcons(),
    staleTime: 60000,
    gcTime: 1800000,
    refetchInterval: false,
    refetchIntervalInBackground: false,
    placeholderData: (previousData) => previousData,
  })

  return query
}
