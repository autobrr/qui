/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedRoutes } from "@/hooks/useCrossSeedRoutes"
import { useInstances } from "@/hooks/useInstances"

export function useCrossSeedInstanceOf(instanceId: number, enabled = true): { id: number; name: string } | undefined {
  const { data: routes } = useCrossSeedRoutes(enabled)
  const { instances } = useInstances()
  const id = routes?.find(route => route.sourceInstanceId === instanceId)?.crossSeedInstanceId
  const crossSeed = instances?.find(instance => instance.id === id)
  return crossSeed ? { id: crossSeed.id, name: crossSeed.name } : undefined
}
