/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import type { Torrent, TorrentStreamPayload } from "@/types"
import { makeTorrent } from "@/test/mockTorrent"
import { TorrentDetailsPanel } from "./TorrentDetailsPanel"

const mocks = vi.hoisted(() => ({
  state: { connected: true, initialized: true, dataStalled: false, error: null, retrying: false, retryAttempt: 0 } as StreamState,
  onMessage: undefined as ((payload: TorrentStreamPayload) => void) | undefined,
  getTorrents: vi.fn(),
  metadata: { data: { categories: {}, tags: [] } },
  capabilities: { data: {} },
  matches: { matchingTorrents: [], isLoadingMatches: false, allInstances: [] },
  renders: [] as number[],
}))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: (_params: unknown, options: { onMessage: typeof mocks.onMessage }) => {
    mocks.onMessage = options.onMessage
    return mocks.state
  },
}))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => mocks.metadata }))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => mocks.capabilities }))
vi.mock("@/lib/cross-seed-utils", () => ({
  useLocalCrossSeedMatches: () => mocks.matches,
  isHardlinkManaged: () => false,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: async () => [],
    getTorrentProperties: async () => ({}),
    getTorrentTrackers: async () => [],
    getTorrents: (...args: unknown[]) => mocks.getTorrents(...args),
  },
}))
vi.mock("./details", () => ({
  GeneralTabHorizontal: ({ torrent }: { torrent: Torrent }) => {
    mocks.renders.push(torrent.progress)
    return <div data-testid="live-progress">{torrent.progress}</div>
  },
  CrossSeedTable: () => null,
  PeersTable: () => null,
  TorrentFileTable: () => null,
  TrackerContextMenu: () => null,
  TrackersTable: () => null,
  WebSeedsTable: () => null,
}))

beforeEach(() => {
  mocks.renders.length = 0
  mocks.onMessage = undefined
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe("TorrentDetailsPanel fallback", () => {
  it.each(["stalled", "uninitialized"])("polls and replaces retained rows when %s", async condition => {
    mocks.state = { connected: true, initialized: true, dataStalled: false, error: null, retrying: false, retryAttempt: 0 }
    const torrent = makeTorrent({ hash: "a", progress: 0 })
    mocks.getTorrents.mockResolvedValue({ torrents: [{ ...torrent, progress: 1 }] })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = () => (
      <QueryClientProvider client={client}>
        <TorrentDetailsPanel instanceId={1} torrent={torrent} layout="horizontal" onClose={vi.fn()} />
      </QueryClientProvider>
    )
    const { getByTestId, rerender } = render(view())
    await waitFor(() => expect(mocks.onMessage).toBeDefined())
    act(() => mocks.onMessage?.({ type: "init", data: { torrents: [{ ...torrent, progress: 0.5 }], total: 1 } }))
    await waitFor(() => expect(getByTestId("live-progress").textContent).toBe("0.5"))
    expect(mocks.getTorrents).not.toHaveBeenCalled()

    mocks.state = { ...mocks.state, initialized: condition !== "uninitialized", dataStalled: condition === "stalled" }
    mocks.renders.length = 0
    rerender(view())
    await waitFor(() => expect(getByTestId("live-progress").textContent).toBe("1"))
    expect(mocks.getTorrents).toHaveBeenCalledWith(1, expect.objectContaining({ limit: 1 }))
    expect(mocks.renders).not.toContain(0.5)
    client.clear()
  })

  it("does not reuse a snapshot for the same hash on another instance", async () => {
    mocks.state = { connected: true, initialized: true, dataStalled: false, error: null, retrying: false, retryAttempt: 0 }
    const torrent = makeTorrent({ hash: "a", progress: 0 })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = (instanceId: number) => (
      <QueryClientProvider client={client}>
        <TorrentDetailsPanel instanceId={instanceId} torrent={torrent} layout="horizontal" onClose={vi.fn()} />
      </QueryClientProvider>
    )
    const { getByTestId, rerender } = render(view(1))
    await waitFor(() => expect(mocks.onMessage).toBeDefined())
    act(() => mocks.onMessage?.({ type: "init", data: { torrents: [{ ...torrent, progress: 0.5 }], total: 1 } }))
    await waitFor(() => expect(getByTestId("live-progress").textContent).toBe("0.5"))
    mocks.renders.length = 0
    rerender(view(2))
    await waitFor(() => expect(getByTestId("live-progress").textContent).toBe("0"))
    expect(mocks.renders).not.toContain(0.5)
    act(() => mocks.onMessage?.({ type: "init", data: { torrents: [{ ...torrent, progress: 0.8 }], total: 1 } }))
    expect(getByTestId("live-progress").textContent).toBe("0.8")
    client.clear()
  })
})
