/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"

/**
 * The Cross-seed instance that receives the cross-seeds found on instanceId,
 * or undefined when instanceId has no route (ADR 0035).
 */
export function useCrossSeedInstanceOf(instanceId: number, enabled = true): { id: number; name: string } | undefined {
  const { data: routes } = useQuery({
    queryKey: ["cross-seed", "routes"],
    queryFn: () => api.listCrossSeedRoutes(),
    enabled,
    staleTime: 60_000,
  })
  const { instances } = useInstances()
  const id = routes?.find(route => route.sourceInstanceId === instanceId)?.crossSeedInstanceId
  const crossSeed = instances?.find(instance => instance.id === id)
  return crossSeed ? { id: crossSeed.id, name: crossSeed.name } : undefined
}
