/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentStreamPayload } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { ReactNode } from "react"

const mocks = vi.hoisted(() => ({
  state: {
    connected: true,
    initialized: true,
    dataStalled: false,
    error: null,
    retrying: false,
    retryAttempt: 0,
  },
  onMessage: undefined as undefined | ((payload: TorrentStreamPayload) => void),
  getActiveTaskCount: vi.fn(),
}))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: (_params: unknown, options: { onMessage: (payload: TorrentStreamPayload) => void }) => {
    mocks.onMessage = options.onMessage
    return mocks.state
  },
}))
vi.mock("@/lib/api", () => ({ api: { getActiveTaskCount: mocks.getActiveTaskCount } }))

import { useActiveTaskCount } from "@/hooks/useActiveTaskCount"

beforeEach(() => {
  mocks.state = { ...mocks.state, connected: true, initialized: true, dataStalled: false, error: null }
  mocks.getActiveTaskCount.mockReset().mockResolvedValue(2)
})
afterEach(cleanup)

describe("useActiveTaskCount", () => {
  it("uses REST while a connected stream is stalled and resumes streamed counts after recovery", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result, rerender, unmount } = renderHook(() => useActiveTaskCount(1, true), { wrapper })

    act(() => mocks.onMessage?.({ type: "init", data: { torrents: [], total: 0, activeTaskCount: 7 } }))
    expect(result.current).toBe(7)

    mocks.state = { ...mocks.state, dataStalled: true }
    rerender()
    await waitFor(() => expect(result.current).toBe(2))
    expect(mocks.getActiveTaskCount).toHaveBeenCalledWith(1)

    mocks.state = { ...mocks.state, dataStalled: false }
    rerender()
    act(() => mocks.onMessage?.({ type: "update", data: { torrents: [], total: 0, activeTaskCount: 3 } }))
    expect(result.current).toBe(3)
    unmount()
    client.clear()
  })

  it("does not show another instance's streamed count while the next instance loads", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result, rerender, unmount } = renderHook(
      ({ instanceId }) => useActiveTaskCount(instanceId, true),
      { initialProps: { instanceId: 1 }, wrapper }
    )

    act(() => mocks.onMessage?.({ type: "init", data: { torrents: [], total: 0, activeTaskCount: 7 } }))
    expect(result.current).toBe(7)

    rerender({ instanceId: 2 })
    await waitFor(() => expect(result.current).toBe(2))
    expect(mocks.getActiveTaskCount).toHaveBeenCalledWith(2)
    unmount()
    client.clear()
  })
})
