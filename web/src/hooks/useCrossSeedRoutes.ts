/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { Instance } from "@/types"
import { useQuery } from "@tanstack/react-query"

/** Invalidate crossSeedRoutesKey after a route save or clear: it also covers each route read. */
export const crossSeedRoutesKey = ["cross-seed", "routes"] as const

/** Every saved route, so a view knows which instances route to which (ADR 0035). */
export function useCrossSeedRoutes(enabled = true, staleTime = 60_000) {
  return useQuery({
    queryKey: crossSeedRoutesKey,
    queryFn: () => api.listCrossSeedRoutes(),
    enabled,
    staleTime,
  })
}

/**
 * The route read of a Source instance: its rows with the routed torrent counts
 * and the rule status, or null when it has no route. The key holds the saved
 * base folders, so a base folder that the card saves shows as a new row.
 */
export function useCrossSeedRoute(instance: Pick<Instance, "id" | "hardlinkBaseDir">, enabled = true) {
  return useQuery({
    queryKey: [...crossSeedRoutesKey, instance.id, instance.hardlinkBaseDir],
    queryFn: () => api.getCrossSeedRoute(instance.id),
    enabled,
  })
}
