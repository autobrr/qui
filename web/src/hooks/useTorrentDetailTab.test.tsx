/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import { STREAM_ERROR_DISCONNECTED } from "@/lib/sync-stream-state"
import { makeTorrent } from "@/test/mockTorrent"
import type { TorrentProperties, TorrentStreamPayload } from "@/types"
import { useTorrentDetailTab, type TorrentDetailTab } from "./useTorrentDetailTab"

const usableStream: StreamState = { connected: true, initialized: true, dataStalled: false, error: null, retrying: false, retryAttempt: 0 }

const mocks = vi.hoisted(() => ({
  state: undefined as unknown as StreamState,
  onMessage: undefined as ((payload: TorrentStreamPayload) => void) | undefined,
  getTorrentProperties: vi.fn(),
  getTorrentPeers: vi.fn(),
  getTorrentWebSeeds: vi.fn(),
  getTorrents: vi.fn(),
  params: [] as unknown[],
}))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: (params: unknown, options: { onMessage: typeof mocks.onMessage }) => {
    mocks.params.push(params)
    if (params) {
      mocks.onMessage = options.onMessage
    }
    return mocks.state
  },
}))
vi.mock("@/lib/api", () => ({
  api: {
    getTorrentProperties: (...args: unknown[]) => mocks.getTorrentProperties(...args),
    getTorrentPeers: (...args: unknown[]) => mocks.getTorrentPeers(...args),
    getTorrentWebSeeds: (...args: unknown[]) => mocks.getTorrentWebSeeds(...args),
    getTorrents: (...args: unknown[]) => mocks.getTorrents(...args),
  },
}))

const loadedProperties = {
  comment: "Built from the source",
  created_by: "mkbrr",
  save_path: "/data/seeding",
  pieces_have: 40,
  pieces_num: 100,
  dl_speed: 1,
  up_speed: 1,
  seeds: 0,
  seeds_total: 0,
  peers: 0,
  peers_total: 0,
  share_ratio: 0,
  total_downloaded: 0,
  total_uploaded: 0,
  time_elapsed: 1,
  seeding_time: 1,
} as TorrentProperties

let client: QueryClient

function renderTab<T extends TorrentDetailTab>(tab: T, instanceId = 1, hash = "a") {
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return renderHook(props => useTorrentDetailTab(props.instanceId, props.hash, tab), { wrapper, initialProps: { instanceId, hash } })
}

function sendRow(row: Parameters<typeof makeTorrent>[0]) {
  act(() => mocks.onMessage?.({ type: "update", data: { torrents: [makeTorrent({ hash: "a", ...row })], total: 1 } }))
}

function setPageHidden(hidden: boolean) {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => hidden ? "hidden" : "visible" })
  window.dispatchEvent(new Event("visibilitychange"))
}

beforeEach(() => {
  mocks.state = usableStream
  mocks.onMessage = undefined
  mocks.getTorrentProperties.mockResolvedValue(loadedProperties)
  mocks.getTorrentPeers.mockResolvedValue({ peers: {}, sorted_peers: [] })
  mocks.getTorrentWebSeeds.mockResolvedValue([{ url: "https://example.invalid/seed" }])
  mocks.getTorrents.mockResolvedValue({ torrents: [] })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.clearAllMocks()
  vi.useRealTimers()
  setPageHidden(false)
})

async function advance(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
}

describe("useTorrentDetailTab general", () => {
  it("merges the live row over the fields that load one time", async () => {
    const { result } = renderTab("general")
    await waitFor(() => expect(result.current.data?.properties).toBeDefined())
    sendRow({ dlspeed: 500, upspeed: 70, num_seeds: 3, num_complete: 9, num_leechs: 2, num_incomplete: 4, ratio: 1.5, downloaded: 1000, uploaded: 1500, time_active: 100, seeding_time: 50 })

    expect(result.current.data?.properties).toEqual({
      ...loadedProperties,
      dl_speed: 500,
      up_speed: 70,
      seeds: 3,
      seeds_total: 9,
      peers: 2,
      peers_total: 4,
      share_ratio: 1.5,
      total_downloaded: 1000,
      total_uploaded: 1500,
      time_elapsed: 100,
      seeding_time: 50,
    })
    expect(result.current.data?.row?.dlspeed).toBe(500)
  })

  it("shows live piece counts from a qBittorrent 5.2 row", async () => {
    const { result } = renderTab("general")
    await waitFor(() => expect(result.current.data?.properties).toBeDefined())
    sendRow({ state: "downloading", pieces_have: 60, pieces_num: 100 })
    expect([result.current.data?.properties?.pieces_have, result.current.data?.properties?.pieces_num]).toEqual([60, 100])

    sendRow({ state: "downloading", pieces_have: 75, pieces_num: 100 })
    expect([result.current.data?.properties?.pieces_have, result.current.data?.properties?.pieces_num]).toEqual([75, 100])
    expect(mocks.getTorrentProperties).toHaveBeenCalledTimes(1)
  })

  it("keeps the loaded piece counts with a row from before qBittorrent 5.2", async () => {
    const { result } = renderTab("general")
    await waitFor(() => expect(result.current.data?.properties).toBeDefined())
    // qui sends 0 for both fields when qBittorrent does not send them.
    sendRow({ state: "downloading", pieces_have: 0, pieces_num: 0 })
    expect([result.current.data?.properties?.pieces_have, result.current.data?.properties?.pieces_num]).toEqual([40, 100])
    expect(mocks.getTorrentProperties).toHaveBeenCalledTimes(1)
  })

  it("shows live time active, seeds, and peers on a seeding torrent without a new fetch", async () => {
    const { result } = renderTab("general")
    await waitFor(() => expect(result.current.data?.properties).toBeDefined())
    sendRow({ state: "uploading", time_active: 100, seeding_time: 50, num_seeds: 1, num_leechs: 2 })
    sendRow({ state: "uploading", time_active: 105, seeding_time: 55, num_seeds: 4, num_leechs: 6 })

    const properties = result.current.data?.properties
    expect([properties?.time_elapsed, properties?.seeding_time, properties?.seeds, properties?.peers]).toEqual([105, 55, 4, 6])
    expect(mocks.getTorrentProperties).toHaveBeenCalledTimes(1)
    expect(mocks.getTorrents).not.toHaveBeenCalled()
  })

  it("keeps the previous row on a delta with no changed rows", async () => {
    const { result } = renderTab("general")
    sendRow({ time_active: 100 })
    act(() => mocks.onMessage?.({ type: "delta", data: { torrents: [], total: 1 } }))
    expect(result.current.data?.row?.time_active).toBe(100)
  })

  it("does not reuse a row for the same hash on another instance", async () => {
    const { result, rerender } = renderTab("general")
    sendRow({ time_active: 100 })
    expect(result.current.data?.row?.time_active).toBe(100)

    rerender({ instanceId: 2, hash: "a" })
    expect(result.current.data?.row).toBeNull()
  })

  it.each([
    ["a stalled view", { dataStalled: true }, 1, 2],
    ["a server stream-error", { connected: false, error: "instance unreachable: connection refused" }, 1, 1],
    ["a client-side connection error", { connected: false, error: STREAM_ERROR_DISCONNECTED }, 5, 16],
    ["an uninitialized stream", { initialized: false }, 5, 16],
  ] as const)("polls the row at the stream fallback interval for %s", async (_name, state, callsAt9s, callsAt30s) => {
    vi.useFakeTimers()
    mocks.state = { ...usableStream, ...state }
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", time_active: 300 })] })
    const { result } = renderTab("general")

    await advance(9000)
    expect(mocks.getTorrents).toHaveBeenCalledTimes(callsAt9s)
    expect(mocks.getTorrents).toHaveBeenCalledWith(1, expect.objectContaining({ limit: 1 }))
    expect(result.current.data?.properties?.time_elapsed).toBe(300)
    await advance(21000)
    expect(mocks.getTorrents).toHaveBeenCalledTimes(callsAt30s)
  })

  it.each([
    ["a usable stream", usableStream],
    ["a disconnected stream", { ...usableStream, connected: false, error: STREAM_ERROR_DISCONNECTED }],
  ] as const)("follows the live hash before the panel is ready and fetches nothing, with %s", async (_name, state) => {
    vi.useFakeTimers()
    mocks.state = state
    mocks.params.length = 0
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    renderHook(() => useTorrentDetailTab(1, null, "general", "a"), { wrapper })
    await advance(1000)
    expect(mocks.params).toContainEqual(expect.objectContaining({ filters: expect.objectContaining({ hashes: ["a"] }) }))
    expect(mocks.getTorrentProperties).not.toHaveBeenCalled()
    expect(mocks.getTorrents).not.toHaveBeenCalled()
  })

  it("replaces the stream row with the polled row when the view stalls", async () => {
    const { result, rerender } = renderTab("general")
    sendRow({ time_active: 100 })
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", time_active: 400 })] })

    mocks.state = { ...usableStream, dataStalled: true }
    rerender({ instanceId: 1, hash: "a" })
    await waitFor(() => expect(result.current.data?.row?.time_active).toBe(400))
  })
})

describe("useTorrentDetailTab peers and HTTP sources", () => {
  it("polls peers every 2 s", async () => {
    vi.useFakeTimers()
    const { result } = renderTab("peers")
    await advance(0)
    expect(result.current.data).toEqual({ peers: {}, sorted_peers: [] })
    await advance(6000)
    expect(mocks.getTorrentPeers).toHaveBeenCalledTimes(4)
    expect(mocks.getTorrentPeers).toHaveBeenCalledWith(1, "a")
  })

  it("loads HTTP sources one time", async () => {
    vi.useFakeTimers()
    const { result } = renderTab("webseeds")
    await advance(60000)
    expect(result.current.data).toEqual([{ url: "https://example.invalid/seed" }])
    expect(mocks.getTorrentWebSeeds).toHaveBeenCalledTimes(1)
  })

  it("does not fetch with a null hash", async () => {
    vi.useFakeTimers()
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    renderHook(() => useTorrentDetailTab(1, null, "peers"), { wrapper })
    await advance(6000)
    expect(mocks.getTorrentPeers).not.toHaveBeenCalled()
  })
})

describe("useTorrentDetailTab hidden page", () => {
  it.each([
    ["peers", () => mocks.getTorrentPeers, usableStream],
    ["general", () => mocks.getTorrents, { ...usableStream, connected: false, error: STREAM_ERROR_DISCONNECTED }],
  ] as const)("does not poll %s while the page is hidden", async (tab, fetcher, state) => {
    vi.useFakeTimers()
    mocks.state = state
    renderTab(tab)
    await advance(0)
    const calls = fetcher().mock.calls.length
    expect(calls).toBeGreaterThan(0)

    setPageHidden(true)
    await advance(10000)
    expect(fetcher()).toHaveBeenCalledTimes(calls)

    setPageHidden(false)
    await advance(2000)
    expect(fetcher().mock.calls.length).toBeGreaterThan(calls)
  })
})
