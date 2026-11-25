/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import { isDemo } from "@/lib/demo"
import type { ExperimentalFeature } from "@/types"

export function useExperimentalFeature(feature: ExperimentalFeature): boolean {
  const { data, isError } = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
    enabled: !isDemo,
  })
  return !isDemo && !isError && (data?.experimentalFeatures.includes(feature) ?? false)
}
