/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { Instance } from "@/types"
import { useQuery } from "@tanstack/react-query"

export const crossSeedRoutesKey = ["cross-seed", "routes"] as const

export function useCrossSeedRoutes(enabled = true, staleTime = 60_000) {
  return useQuery({
    queryKey: crossSeedRoutesKey,
    queryFn: () => api.listCrossSeedRoutes(),
    enabled,
    staleTime,
  })
}

export function crossSeedRouteKey(instance: Pick<Instance, "id" | "hardlinkBaseDir">) {
  return [...crossSeedRoutesKey, instance.id, instance.hardlinkBaseDir]
}

export function useCrossSeedRoute(instance: Pick<Instance, "id" | "hardlinkBaseDir">, enabled = true) {
  return useQuery({
    queryKey: crossSeedRouteKey(instance),
    queryFn: () => api.getCrossSeedRoute(instance.id),
    enabled,
  })
}
