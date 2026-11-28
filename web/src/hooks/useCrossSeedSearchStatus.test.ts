/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedSearchStatus } from "@/types"
import { QueryClient } from "@tanstack/react-query"
import { beforeEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  getCrossSeedSearchStatus: vi.fn(),
}))

vi.mock("@/lib/api", () => ({ api: { getCrossSeedSearchStatus: mocks.getCrossSeedSearchStatus } }))

import { crossSeedSearchStatusQuery } from "@/hooks/useCrossSeedSettings"

function running(instanceId: number): CrossSeedSearchStatus {
  return { running: true, run: { instanceId } } as CrossSeedSearchStatus
}

const idle = { running: false } as CrossSeedSearchStatus

function runsInvalidated(client: QueryClient, instanceId: number): boolean | undefined {
  return client.getQueryState(["cross-seed", "search-runs", instanceId])?.isInvalidated
}

async function poll(before: CrossSeedSearchStatus | undefined, after: CrossSeedSearchStatus): Promise<QueryClient> {
  const client = new QueryClient()
  client.setQueryData(["cross-seed", "search-runs", 1], [])
  client.setQueryData(["cross-seed", "search-runs", 2], [])
  if (before) client.setQueryData(crossSeedSearchStatusQuery.queryKey, before)
  mocks.getCrossSeedSearchStatus.mockResolvedValue(after)
  await client.fetchQuery(crossSeedSearchStatusQuery)
  return client
}

beforeEach(() => {
  mocks.getCrossSeedSearchStatus.mockReset()
})

describe("crossSeedSearchStatusQuery", () => {
  it("marks the runs of the instance stale when its search run ends", async () => {
    const client = await poll(running(1), idle)
    expect(runsInvalidated(client, 1)).toBe(true)
    expect(runsInvalidated(client, 2)).toBe(false)
  })

  it("leaves the runs alone while the search run goes on", async () => {
    const client = await poll(running(1), running(1))
    expect(runsInvalidated(client, 1)).toBe(false)
  })

  it("leaves the runs alone when no search run was running", async () => {
    const client = await poll(undefined, idle)
    expect(runsInvalidated(client, 1)).toBe(false)
    const idleClient = await poll(idle, idle)
    expect(runsInvalidated(idleClient, 1)).toBe(false)
  })
})
