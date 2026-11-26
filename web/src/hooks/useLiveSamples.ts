/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { LIVE_SLOT_MS } from "@/lib/dashboard-overview"
import { useQuery } from "@tanstack/react-query"

export function useLiveSamples(enabled: boolean) {
  return useQuery({
    queryKey: ["live-samples"],
    queryFn: () => api.getLiveSamples(),
    enabled,
    staleTime: 0,
    refetchInterval: LIVE_SLOT_MS,
    refetchOnWindowFocus: true,
  })
}
