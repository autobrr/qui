/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query"
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
  getTorrentFiles: vi.fn(),
  getTorrentPieceStates: vi.fn(),
  getTorrentTrackers: vi.fn(),
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
    getTorrentFiles: (...args: unknown[]) => mocks.getTorrentFiles(...args),
    getTorrentPieceStates: (...args: unknown[]) => mocks.getTorrentPieceStates(...args),
    getTorrentTrackers: (...args: unknown[]) => mocks.getTorrentTrackers(...args),
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

function makeFiles(count: number) {
  return Array.from({ length: count }, (_, index) => ({ index, name: `file-${index}.mkv` }))
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
  mocks.getTorrentTrackers.mockResolvedValue([{ url: "https://tracker.example.invalid/announce", status: 2 }])
  mocks.getTorrents.mockResolvedValue({ torrents: [] })
  mocks.getTorrentFiles.mockResolvedValue(makeFiles(500))
  mocks.getTorrentPieceStates.mockResolvedValue(new Array(20000).fill(1))
  // The app turns off the refetch on focus; the hidden-page tests rely on that.
  client = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } })
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

describe("useTorrentDetailTab content and piece bar", () => {
  // Opens the tab on a downloading torrent, and returns once the first fetch
  // and the first row are in.
  async function openDownloading(tab: "content" | "pieces") {
    vi.useFakeTimers()
    const view = renderTab(tab)
    await advance(0)
    sendRow({ progress: 0.1, state: "downloading" })
    return view
  }

  it.each([
    ["500", 500, 5000],
    ["20,000", 20000, 20000],
  ] as const)("reloads Content with %s files on a progress change after the minimum gap", async (_name, count, gap) => {
    mocks.getTorrentFiles.mockResolvedValue(makeFiles(count))
    const { result } = await openDownloading("content")
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)

    const reloaded = makeFiles(count + 1)
    mocks.getTorrentFiles.mockResolvedValue(reloaded)
    sendRow({ progress: 0.2, state: "downloading" })
    await advance(gap - 100)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)
    await advance(100)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
    // React Query hands the result to the hook on the next timer.
    await advance(1)
    expect(result.current.data).toEqual(reloaded)

    // The next change waits a full gap after the reload.
    sendRow({ progress: 0.3, state: "downloading" })
    await advance(gap - 100)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    await advance(100)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(3)
  })

  it("reads past the file cache on the first Content fetch", async () => {
    await openDownloading("content")
    expect(mocks.getTorrentFiles).toHaveBeenCalledWith(1, "a", { refresh: true })
  })

  it("reads past the file cache on the first Content fetch of the next torrent", async () => {
    vi.useFakeTimers()
    const view = renderTab("content")
    await advance(0)
    sendRow({ progress: 1, state: "stalledUP" })
    view.rerender({ instanceId: 2, hash: "a" })
    act(() => mocks.onMessage?.({ type: "update", data: { torrents: [makeTorrent({ hash: "a", progress: 1, state: "stalledUP" })], total: 1 } }))
    await advance(0)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(2, "a", { refresh: true })
  })

  it("reads past the file cache on the first Content fetch of the next torrent while the stream is down", async () => {
    vi.useFakeTimers()
    mocks.state = { ...usableStream, connected: false, error: STREAM_ERROR_DISCONNECTED }
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", progress: 1, state: "stalledUP" })] })
    const view = renderTab("content")
    await advance(0)
    view.rerender({ instanceId: 2, hash: "a" })
    await advance(0)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(2, "a", { refresh: true })
  })

  it("tries Content again after the first fetch fails", async () => {
    vi.useFakeTimers()
    mocks.getTorrentFiles.mockRejectedValueOnce(new Error("bad gateway"))
    renderTab("content")
    await advance(0)
    sendRow({ progress: 1, state: "stalledUP" })
    await advance(6000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it("reloads Content when the row changes while the first fetch runs", async () => {
    vi.useFakeTimers()
    let resolve!: (files: unknown) => void
    mocks.getTorrentFiles.mockReturnValueOnce(new Promise(r => { resolve = r }))
    renderTab("content")
    await advance(0)
    sendRow({ progress: 0.99, state: "downloading" })
    sendRow({ progress: 1, state: "stalledUP" })
    await act(async () => resolve(makeFiles(500)))
    await advance(6000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it("reloads Content past the file cache when the stream comes back after a change", async () => {
    vi.useFakeTimers()
    mocks.state = { ...usableStream, connected: false, error: STREAM_ERROR_DISCONNECTED }
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", progress: 0.5, state: "downloading" })] })
    const view = renderTab("content")
    await advance(0)
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", progress: 1, state: "stalledUP" })] })
    await advance(6000)
    mocks.state = usableStream
    view.rerender({ instanceId: 1, hash: "a" })
    sendRow({ progress: 1, state: "stalledUP" })
    await advance(5000)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it("keeps the cache bypass when a Content reload fails", async () => {
    client = new QueryClient({ defaultOptions: { queries: { retry: 1, retryDelay: 0 } } })
    await openDownloading("content")
    mocks.getTorrentFiles.mockRejectedValueOnce(new Error("bad gateway"))
    sendRow({ progress: 1, state: "stalledUP" })
    // React Query retries the failed reload one time.
    await advance(6000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(3)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it("reads past the file cache on another fetch while a reload waits", async () => {
    await openDownloading("content")
    sendRow({ progress: 1, state: "stalledUP" })
    await advance(100)
    await act(() => client.invalidateQueries({ queryKey: ["torrent-files"] }))
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
    await advance(5000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
  })

  it("reloads Content on a state change", async () => {
    await openDownloading("content")
    sendRow({ progress: 0.1, state: "stoppedDL" })
    await advance(5000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
  })

  it("reloads the piece bar on a progress change after 5 s", async () => {
    const { result } = await openDownloading("pieces")
    const reloaded = new Array(20000).fill(2)
    mocks.getTorrentPieceStates.mockResolvedValue(reloaded)
    sendRow({ progress: 0.2, state: "downloading" })
    await advance(4900)
    expect(mocks.getTorrentPieceStates).toHaveBeenCalledTimes(1)
    await advance(100)
    expect(mocks.getTorrentPieceStates).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentPieceStates).toHaveBeenLastCalledWith(1, "a")
    // React Query hands the result to the hook on the next timer.
    await advance(1)
    expect(result.current.data).toEqual(reloaded)
  })

  it("does not reload Content on a torrent that only seeds", async () => {
    vi.useFakeTimers()
    renderTab("content")
    await advance(0)
    for (const [state, time] of [["uploading", 100], ["stalledUP", 200], ["uploading", 300], ["forcedUP", 400]] as const) {
      sendRow({ progress: 1, state, time_active: time, num_seeds: time, num_leechs: time })
      await advance(10000)
    }
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)
  })

  it.each([
    ["content", () => mocks.getTorrentFiles, 3000, [1, "a", { refresh: true }]],
    ["pieces", () => mocks.getTorrentPieceStates, 5000, [1, "a"]],
  ] as const)("polls %s as before while the torrent is checking", async (tab, fetcher, interval, args) => {
    vi.useFakeTimers()
    renderTab(tab)
    sendRow({ progress: 0.4, state: "checkingDL" })
    await advance(0)
    const calls = fetcher().mock.calls.length
    await advance(interval * 3)
    expect(fetcher()).toHaveBeenCalledTimes(calls + 3)
    expect(fetcher()).toHaveBeenLastCalledWith(...args)
  })

  it("reloads Content when a check ends on a complete torrent", async () => {
    vi.useFakeTimers()
    renderTab("content")
    sendRow({ progress: 1, state: "checkingUP" })
    await advance(0)
    const calls = mocks.getTorrentFiles.mock.calls.length
    sendRow({ progress: 1, state: "stalledUP" })
    await advance(5000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(calls + 1)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it("waits for the page to show before a reload", async () => {
    await openDownloading("content")
    setPageHidden(true)
    sendRow({ progress: 0.2, state: "downloading" })
    await advance(60000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)

    setPageHidden(false)
    await advance(0)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentFiles).toHaveBeenLastCalledWith(1, "a", { refresh: true })
  })

  it.each([
    ["the checking poll", usableStream],
    ["the fallback poll", { ...usableStream, connected: false, error: STREAM_ERROR_DISCONNECTED }],
  ] as const)("does not run %s of Content while the page is hidden", async (_name, state) => {
    vi.useFakeTimers()
    mocks.state = state
    renderTab("content")
    sendRow({ progress: 0.4, state: "checkingDL" })
    await advance(0)
    setPageHidden(true)
    const calls = mocks.getTorrentFiles.mock.calls.length
    await advance(30000)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(calls)

    setPageHidden(false)
    await advance(3000)
    expect(mocks.getTorrentFiles.mock.calls.length).toBeGreaterThan(calls)
  })

  it.each([
    ["a client-side connection error", { connected: false, error: STREAM_ERROR_DISCONNECTED }, 3],
    ["a stalled view", { dataStalled: true }, 0],
    ["a server stream-error", { connected: false, error: "instance unreachable: connection refused" }, 0],
  ] as const)("polls Content at the stream fallback interval for %s", async (_name, state, pollsIn9s) => {
    vi.useFakeTimers()
    mocks.state = { ...usableStream, ...state }
    renderTab("content")
    await advance(0)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)
    await advance(9000)
    expect(mocks.getTorrentFiles.mock.calls.slice(1)).toEqual(new Array(pollsIn9s).fill([1, "a", { refresh: false }]))
  })
})

describe("useTorrentDetailTab trackers", () => {
  async function renderLoadedTrackers() {
    const rendered = renderTab("trackers")
    await waitFor(() => expect(rendered.result.current.data).toEqual([{ url: "https://tracker.example.invalid/announce", status: 2 }]))
    sendRow({ trackers_count: 2 })
    return rendered
  }

  it("reloads when the Tracker health of the row changes", async () => {
    await renderLoadedTrackers()
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(1)

    sendRow({ trackers_count: 2, tracker_health: "tracker_down" })
    await waitFor(() => expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(2))
    expect(mocks.getTorrentTrackers).toHaveBeenLastCalledWith(1, "a")
  })

  it("reloads when the tracker count of the row changes", async () => {
    await renderLoadedTrackers()
    sendRow({ trackers_count: 3 })
    await waitFor(() => expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(2))
  })

  it("does not reload on other row changes", async () => {
    vi.useFakeTimers()
    renderTab("trackers")
    await advance(0)
    sendRow({ trackers_count: 2, dlspeed: 100, num_seeds: 3, time_active: 10, state: "downloading" })
    sendRow({ trackers_count: 2, dlspeed: 900, num_seeds: 8, time_active: 20, state: "stalledDL" })
    await advance(60000)
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(1)
  })

  it("does not reload on a switch to another torrent", async () => {
    vi.useFakeTimers()
    const { rerender } = renderTab("trackers")
    await advance(0)
    sendRow({ trackers_count: 2 })
    rerender({ instanceId: 1, hash: "b" })
    await advance(0)
    act(() => mocks.onMessage?.({ type: "update", data: { torrents: [makeTorrent({ hash: "b", trackers_count: 5 })], total: 1 } }))
    await advance(60000)
    expect(mocks.getTorrentTrackers.mock.calls).toEqual([[1, "a"], [1, "b"]])
  })

  it("reloads on a return to a torrent whose Tracker health changed while another torrent was open", async () => {
    vi.useFakeTimers()
    const { rerender } = renderTab("trackers")
    await advance(0)
    sendRow({ trackers_count: 2 })
    rerender({ instanceId: 1, hash: "b" })
    await advance(0)
    act(() => mocks.onMessage?.({ type: "update", data: { torrents: [makeTorrent({ hash: "b", trackers_count: 5 })], total: 1 } }))
    // The list of a is still fresh, so the return alone does not fetch it.
    rerender({ instanceId: 1, hash: "a" })
    await advance(0)
    sendRow({ trackers_count: 2, tracker_health: "unregistered" })
    await advance(0)
    expect(mocks.getTorrentTrackers.mock.calls).toEqual([[1, "a"], [1, "b"], [1, "a"]])
  })

  it("does not fetch while the page is hidden, and reloads when it shows again", async () => {
    vi.useFakeTimers()
    renderTab("trackers")
    await advance(0)
    sendRow({ trackers_count: 2 })

    setPageHidden(true)
    sendRow({ trackers_count: 2, tracker_health: "unregistered" })
    await advance(60000)
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(1)

    setPageHidden(false)
    await advance(0)
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(2)
  })

  it("reloads from the polled row when the stream is not usable", async () => {
    vi.useFakeTimers()
    mocks.state = { ...usableStream, dataStalled: true }
    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", trackers_count: 2 })] })
    renderTab("trackers")
    await advance(0)
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(1)

    mocks.getTorrents.mockResolvedValue({ torrents: [makeTorrent({ hash: "a", trackers_count: 2, tracker_health: "tracker_error" })] })
    await advance(31000)
    expect(mocks.getTorrents).toHaveBeenCalledTimes(2)
    expect(mocks.getTorrentTrackers).toHaveBeenCalledTimes(2)
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

  it("does not reload HTTP sources when the page shows again", async () => {
    vi.useFakeTimers()
    renderTab("webseeds")
    await advance(0)
    setPageHidden(true)
    await advance(60000)
    setPageHidden(false)
    await advance(0)
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
  it("does not reload Content on a reconnect while the page is hidden", async () => {
    vi.useFakeTimers()
    renderTab("content")
    await advance(0)
    sendRow({ progress: 0.1, state: "downloading" })
    setPageHidden(true)
    await advance(10000)
    act(() => onlineManager.setOnline(false))
    act(() => onlineManager.setOnline(true))
    await advance(0)
    expect(mocks.getTorrentFiles).toHaveBeenCalledTimes(1)
  })

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
