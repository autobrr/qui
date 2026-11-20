/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { useExperimentalFeature } from "@/hooks/useExperimentalFeature"
import { api } from "@/lib/api"

/** Answers whether an entry point to Problem reports may render: the Experimental feature and the `problemReports` config key are both on. Off in the demo. */
export function useProblemReportsAvailable(): boolean {
  const featureOn = useExperimentalFeature("problemReports")
  const { data } = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
    enabled: featureOn,
  })
  return featureOn && data?.problemReports === true
}
