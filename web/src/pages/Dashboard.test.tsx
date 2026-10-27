/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import { createDashboardStatsFallbackQueryKey } from "@/lib/dashboard-stream"
import { STREAM_ERROR_DISCONNECTED } from "@/lib/sync-stream-state"
import type { InstanceResponse } from "@/types"

const mocks = vi.hoisted(() => ({
  state: undefined as unknown as StreamState,
  getTorrents: vi.fn(),
}))

vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => {
  const manager = {
    connect: () => () => {},
    subscribe: () => () => {},
    getState: () => mocks.state,
    registerActivity: () => () => {},
    subscribeActivity: () => () => {},
  }
  return {
    ...(await importOriginal<typeof import("@/contexts/SyncStreamContext")>()),
    useSyncStreamManager: () => manager,
  }
})
vi.mock("@/lib/api", () => ({ api: { getTorrents: mocks.getTorrents } }))

import { useAllInstanceStats } from "./Dashboard"

const healthy: StreamState = { connected: true, initialized: true, dataStalled: false, error: null, retrying: false, retryAttempt: 0 }
const instances = [{ id: 1, name: "Example", isActive: true }] as InstanceResponse[]

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  mocks.getTorrents.mockReset()
})

describe("useAllInstanceStats fallback poll", () => {
  it.each([
    ["a stalled view", { dataStalled: true }, 0, 1],
    ["a server stream-error", { connected: false, error: "instance unreachable: connection refused" }, 0, 0],
    ["a client-side connection error", { connected: false, error: STREAM_ERROR_DISCONNECTED }, 6, 6],
  ] as const)("polls the stats at the stream fallback interval for %s", async (_name, state, callsAt31s, callsAt33s) => {
    vi.useFakeTimers()
    mocks.state = { ...healthy, ...state }
    mocks.getTorrents.mockResolvedValue({ torrents: [], total: 0 })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { unmount } = renderHook(() => useAllInstanceStats(instances, { enabled: true }), { wrapper })
    // The stream state reaches the poll on the next 2 s flush.
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    mocks.getTorrents.mockClear()

    await act(async () => { await vi.advanceTimersByTimeAsync(29000) })
    expect(mocks.getTorrents).toHaveBeenCalledTimes(callsAt31s)
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(mocks.getTorrents).toHaveBeenCalledTimes(callsAt33s)
    unmount()
    client.clear()
  })

  it("keeps a server stream-error visible over an older stream snapshot", async () => {
    vi.useFakeTimers()
    mocks.state = { ...healthy, connected: false, error: "instance unreachable: connection refused" }
    mocks.getTorrents.mockResolvedValue({ torrents: [], total: 0 })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(createDashboardStatsFallbackQueryKey(1), { torrents: [], total: 0 })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result, unmount } = renderHook(() => useAllInstanceStats(instances, { enabled: true }), { wrapper })
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })

    expect(result.current[0].streamError).toBe("instance unreachable: connection refused")
    unmount()
    client.clear()
  })
})
