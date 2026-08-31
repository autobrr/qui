/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, render, renderHook } from "@testing-library/react"
import { startTransition, Suspense, useEffect } from "react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { makeTorrent } from "@/test/mockTorrent"
import type {
  StreamParams,
  StreamState
} from "@/contexts/SyncStreamContext"
import type {
  AppPreferences,
  CrossInstanceTorrent,
  Torrent,
  TorrentCounts,
  TorrentFilters,
  TorrentResponse,
  TorrentStreamPayload
} from "@/types"

vi.mock("@/lib/api", () => ({
  api: {
    getTorrents: vi.fn(),
    getCrossInstanceTorrents: vi.fn(),
  },
}))

vi.mock("@/hooks/useInstances", () => ({
  useInstances: vi.fn(),
}))

vi.mock("@/hooks/useInstanceCapabilities", () => ({
  useInstanceCapabilities: vi.fn(),
}))

// The stream is mocked at the hook boundary so the REST path is exercised by
// default (disconnected/null state) and tests can drive both the captured
// onMessage handler and the returned StreamState.
let streamState: StreamState
let capturedOnMessage: ((payload: TorrentStreamPayload) => void) | undefined
let lastStreamParams: StreamParams | null
let lastStreamOptions: { enabled?: boolean } | undefined
let hidden = false
let originalHiddenDescriptor: PropertyDescriptor | undefined

// Drives document visibility for the stream-gating tests: the hook pauses the
// list subscription after the tab has been hidden past STREAM_HIDDEN_PAUSE_DELAY_MS.
function setHidden(next: boolean) {
  hidden = next
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"))
  })
}

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: vi.fn(
    (
      params: StreamParams | null,
      options: { enabled?: boolean; onMessage?: (p: TorrentStreamPayload) => void } = {}
    ) => {
      lastStreamParams = params
      lastStreamOptions = options
      capturedOnMessage = options.onMessage
      return streamState
    }
  ),
}))

import { api } from "@/lib/api"
import { useInstances } from "@/hooks/useInstances"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import {
  STREAM_HIDDEN_PAUSE_DELAY_MS,
  TORRENT_STREAM_POLL_INTERVAL_MS,
  useTorrentsList
} from "@/hooks/useTorrentsList"

const LOADED_WINDOW_POLL_INTERVAL_MS = 10_000

const mockedApi = vi.mocked(api, true)
const mockedUseInstances = vi.mocked(useInstances)
const mockedUseCapabilities = vi.mocked(useInstanceCapabilities)

const DISCONNECTED: StreamState = {
  connected: false,
  initialized: false,
  dataStalled: false,
  error: null,
  retrying: false,
  retryAttempt: 0,
}

function makeResponse(overrides: Partial<TorrentResponse> = {}): TorrentResponse {
  return {
    torrents: [],
    total: 0,
    hasMore: false,
    ...overrides,
  }
}

function makeCounts(overrides: Partial<TorrentCounts> = {}): TorrentCounts {
  return {
    status: { all: 1 },
    categories: {},
    tags: {},
    trackers: { "tracker.example": 1 },
    total: 1,
    ...overrides,
  }
}

function makePreferences(overrides: Partial<AppPreferences> = {}): AppPreferences {
  return overrides as AppPreferences
}

function makeWrapper(
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

// fake-timer-friendly flush: pumps pending promises + React effects without the
// real-timer polling that @testing-library's waitFor relies on.
async function flush(times = 8) {
  for (let i = 0; i < times; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  streamState = { ...DISCONNECTED }
  capturedOnMessage = undefined
  lastStreamParams = null
  lastStreamOptions = undefined
  hidden = false
  originalHiddenDescriptor = Object.getOwnPropertyDescriptor(document, "hidden")
  Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => hidden,
  })

  mockedUseInstances.mockReturnValue({
    instances: [
      { id: 1, isActive: true },
      { id: 2, isActive: true },
    ],
  } as unknown as ReturnType<typeof useInstances>)

  mockedUseCapabilities.mockReturnValue({
    data: { supportsSubcategories: true, supportsTorrentCreation: true },
  } as unknown as ReturnType<typeof useInstanceCapabilities>)

  mockedApi.getTorrents.mockResolvedValue(makeResponse())
  mockedApi.getCrossInstanceTorrents.mockResolvedValue(
    makeResponse({ isCrossInstance: true })
  )
})

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
  if (originalHiddenDescriptor) {
    Object.defineProperty(document, "hidden", originalHiddenDescriptor)
  } else {
    Reflect.deleteProperty(document, "hidden")
  }
})

describe("useTorrentsList", () => {
  it("loads the first single-instance page and exposes torrents/total/hasLoadedAll", async () => {
    const torrents = [
      makeTorrent({ hash: "a", name: "alpha" }),
      makeTorrent({ hash: "b", name: "beta" }),
    ]
    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({ torrents, total: 5, hasMore: true })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    expect(result.current.torrents).toHaveLength(2)
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 0, limit: 300 }),
      expect.any(AbortSignal)
    )
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b"])
    expect(result.current.totalCount).toBe(5)
    // hasMore=true -> not all loaded
    expect(result.current.hasLoadedAll).toBe(false)
    expect(result.current.isLoading).toBe(false)
  })

  it("threads the queryFn AbortSignal into the API call so superseded refetches cancel their transfer", async () => {
    renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    const signal = mockedApi.getTorrents.mock.calls[0]?.[2]
    expect(signal).toBeInstanceOf(AbortSignal)
  })

  it("flips hasLoadedAll true when the first page reports no more pages", async () => {
    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({
        torrents: [makeTorrent({ hash: "a" })],
        total: 1,
        hasMore: false,
      })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    expect(result.current.torrents).toHaveLength(1)
    expect(result.current.hasLoadedAll).toBe(true)
  })

  it("clears the list and marks loaded when the first page is empty", async () => {
    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({ torrents: [], total: 0, hasMore: false })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    expect(mockedApi.getTorrents).toHaveBeenCalled()
    expect(result.current.torrents).toEqual([])
    expect(result.current.hasLoadedAll).toBe(true)
    expect(result.current.isLoadingMore).toBe(false)
  })

  it("does not retain pagination loading across a view change", async () => {
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.search === "new") {
        return new Promise(() => undefined)
      }
      if (params.page === 0) {
        return Promise.resolve(makeResponse({ torrents: [makeTorrent({ hash: "old" })], total: 3, hasMore: true }))
      }
      return new Promise(() => undefined)
    })
    const { result, rerender } = renderHook(
      ({ search }) => useTorrentsList(1, { search, pollingEnabled: false }),
      { wrapper: makeWrapper(), initialProps: { search: "old" } }
    )
    await flush()
    act(() => result.current.loadMore())
    await flush()
    expect(result.current.isLoadingMore).toBe(true)
    rerender({ search: "new" })
    await flush()
    expect(result.current.isLoadingMore).toBe(false)
    act(() => capturedOnMessage?.({
      type: "init",
      data: makeResponse({ torrents: [makeTorrent({ hash: "new" })], total: 3, hasMore: true }),
    }))
    await flush()
    expect(result.current.isLoadingMore).toBe(false)
    mockedApi.getTorrents.mockClear()
    act(() => result.current.loadMore())
    await flush()
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(1, expect.objectContaining({ page: 1, search: "new" }), expect.any(AbortSignal))
  })

  it.each([0, 1])("allows pagination during a background refresh of page %i", async currentPage => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let refreshing = false
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (refreshing && (params.page ?? 0) <= currentPage) {
        return new Promise(() => undefined)
      }
      return Promise.resolve(makeResponse({ torrents: [makeTorrent({ hash: `page-${params.page}` })], total: 5, hasMore: true }))
    })
    const { result } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })
    await flush()
    if (currentPage > 0) {
      act(() => result.current.loadMore())
      await flush()
    }
    await act(async () => vi.advanceTimersByTimeAsync(600))
    refreshing = true
    act(() => { void queryClient.refetchQueries({ queryKey: ["torrents-list"], type: "active" }) })
    await flush()
    expect(result.current.isFetching).toBe(true)
    expect(result.current.isLoadingMore).toBe(false)
    act(() => result.current.loadMore())
    await flush()
    expect(mockedApi.getTorrents).toHaveBeenLastCalledWith(1, expect.objectContaining({ page: currentPage + 1 }), expect.any(AbortSignal))
    expect(result.current.torrents.at(-1)?.hash).toBe(`page-${currentPage + 1}`)
  })

  it("waits for the initial page before allowing pagination", async () => {
    mockedApi.getTorrents.mockReturnValue(new Promise(() => undefined))
    const { result } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper() })
    await flush()
    act(() => result.current.loadMore())
    await flush()
    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(1)
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(1, expect.objectContaining({ page: 0 }), expect.any(AbortSignal))
  })

  it("retries a failed page without skipping it or sticking in loading", async () => {
    mockedApi.getTorrents.mockResolvedValueOnce(makeResponse({
      torrents: [makeTorrent({ hash: "a" })], total: 3, hasMore: true,
    })).mockRejectedValueOnce(new Error("offline"))
    const { result } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper() })
    await flush()
    act(() => result.current.loadMore())
    await flush()
    expect(result.current.isLoadingMore).toBe(false)
    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: [makeTorrent({ hash: "b" })], total: 3, hasMore: true }))
    await act(async () => vi.advanceTimersByTimeAsync(600))
    act(() => result.current.loadMore())
    await flush()
    expect(mockedApi.getTorrents).toHaveBeenLastCalledWith(1, expect.objectContaining({ page: 1 }), expect.any(AbortSignal))
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["a", "b"])
    expect(result.current.isLoadingMore).toBe(false)
  })

  it("appends the next page via loadMore without replacing the first page", async () => {
    const page0 = [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })]
    const page1 = [makeTorrent({ hash: "c" }), makeTorrent({ hash: "d" })]

    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({ torrents: page0, total: 4, hasMore: true }))
      }
      return Promise.resolve(makeResponse({ torrents: page1, total: 4, hasMore: false }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    expect(result.current.torrents).toHaveLength(2)

    // Advance past the 500ms throttle window before the first loadMore.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    act(() => {
      result.current.loadMore()
    })

    await flush()

    expect(result.current.torrents).toHaveLength(4)
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c", "d"])
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 1 }),
      expect.any(AbortSignal)
    )
    expect(result.current.hasLoadedAll).toBe(true)
  })

  it("keeps hasLoadedAll false on an appended page when total still exceeds the loaded count", async () => {
    // Isolates the length>=total fallback effect (source lines 312-321) from the
    // hasMore branch on a paginated (page > 0) load. The appended page reports
    // hasMore:false, which on the first page would set hasLoadedAll=true; but only
    // 2 of total:9 rows are loaded, so on an appended page the fallback governs and
    // keeps hasLoadedAll=false. This proves the two mechanisms are independent and
    // that the fallback wins when more rows logically remain.
    const page0 = [makeTorrent({ hash: "a" })]
    const page1 = [makeTorrent({ hash: "b" })]

    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({ torrents: page0, total: 9, hasMore: true }))
      }
      return Promise.resolve(makeResponse({ torrents: page1, total: 9, hasMore: false }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    expect(result.current.torrents).toHaveLength(1)
    expect(result.current.hasLoadedAll).toBe(false)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    expect(result.current.torrents).toHaveLength(2)
    expect(result.current.totalCount).toBe(9)
    // 2 loaded < total 9: the fallback effect overrides the page's hasMore:false.
    expect(result.current.hasLoadedAll).toBe(false)
  })

  it("refreshes rows on every loaded page when active queries refetch after a mutation", async () => {
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: 4,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "c" }), makeTorrent({ hash: "d" })],
        total: 4,
        hasMore: false,
      }))
    })

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper(queryClient) }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c", "d"])

    // The server now reports a new category on a page-0 row and a page-1 row.
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a", category: "tv" }), makeTorrent({ hash: "b" })],
          total: 4,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "c", category: "tv" }), makeTorrent({ hash: "d" })],
        total: 4,
        hasMore: false,
      }))
    })

    // Mirrors the post-mutation refresh every action in useTorrentActions performs.
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ["torrents-list", 1], exact: false, type: "active" })
    })
    await flush()

    const byHash = new Map(result.current.torrents.map(t => [t.hash, t]))
    expect(byHash.get("a")?.category).toBe("tv")
    expect(byHash.get("c")?.category).toBe("tv")
  })

  it("drops a row deleted from a later page once the post-mutation refetch lands", async () => {
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: 4,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "c" }), makeTorrent({ hash: "d" })],
        total: 4,
        hasMore: false,
      }))
    })

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper(queryClient) }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c", "d"])

    // "c" was deleted server-side; later fetches no longer include it.
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: 3,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "d" })],
        total: 3,
        hasMore: false,
      }))
    })

    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ["torrents-list", 1], exact: false, type: "active" })
    })
    await flush()

    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "d"])
    expect(result.current.totalCount).toBe(3)
  })

  it("refreshes later cross-instance pages when active queries refetch after a mutation", async () => {
    const page0Rows = [
      { ...makeTorrent({ hash: "a", name: "alpha" }), instanceId: 1, instanceName: "one" },
    ] as CrossInstanceTorrent[]
    const page1Rows = [
      { ...makeTorrent({ hash: "b", name: "beta" }), instanceId: 2, instanceName: "two" },
    ] as CrossInstanceTorrent[]

    mockedApi.getCrossInstanceTorrents.mockImplementation((params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          isCrossInstance: true,
          cross_instance_torrents: page0Rows,
          total: 2,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        isCrossInstance: true,
        cross_instance_torrents: page1Rows,
        total: 2,
        hasMore: false,
      }))
    })

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(
      () => useTorrentsList(0, { pollingEnabled: false }),
      { wrapper: makeWrapper(queryClient) }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b"])

    // The page-1 torrent was renamed server-side.
    mockedApi.getCrossInstanceTorrents.mockImplementation((params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          isCrossInstance: true,
          cross_instance_torrents: page0Rows,
          total: 2,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        isCrossInstance: true,
        cross_instance_torrents: [
          { ...makeTorrent({ hash: "b", name: "beta-renamed" }), instanceId: 2, instanceName: "two" },
        ] as CrossInstanceTorrent[],
        total: 2,
        hasMore: false,
      }))
    })

    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ["torrents-list", 0], exact: false, type: "active" })
    })
    await flush()

    expect(result.current.torrents.find(t => t.hash === "b")?.name).toBe("beta-renamed")
  })

  it("fetches only the new page on a pagination step, not the whole loaded window", async () => {
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: 6,
          hasMore: true,
        }))
      }
      if (params.page === 1) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "c" }), makeTorrent({ hash: "d" })],
          total: 6,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "e" }), makeTorrent({ hash: "f" })],
        total: 6,
        hasMore: false,
      }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    const pageCalls = () => mockedApi.getTorrents.mock.calls.map(([, params]) => params.page)
    const page0CallsAfterLoad = pageCalls().filter(page => page === 0).length

    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c", "d", "e", "f"])
    // Ordinary scrolling must not refetch already-loaded pages.
    expect(pageCalls().filter(page => page === 0).length).toBe(page0CallsAfterLoad)
    expect(pageCalls().filter(page => page === 1).length).toBe(1)
    expect(pageCalls().filter(page => page === 2).length).toBe(1)
  })

  it("polls every loaded page regardless of row state", async () => {
    let refreshed = false
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: refreshed ? 3 : 4,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: refreshed
          ? [makeTorrent({ hash: "c", state: "uploading", progress: 1, dlspeed: 0 })]
          : [makeTorrent({ hash: "c", state: "downloading", progress: 0.99, dlspeed: 1024 }), makeTorrent({ hash: "d" })],
        total: refreshed ? 3 : 4,
        hasMore: false,
      }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1),
      { wrapper: makeWrapper() }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c", "d"])

    const pageCalls = () => mockedApi.getTorrents.mock.calls.map(([, params]) => params.page)
    const page0CallsAfterLoad = pageCalls().filter(page => page === 0).length
    const page1CallsAfterLoad = pageCalls().filter(page => page === 1).length

    // No checking/moving row is present. The later-page torrent completes and a
    // neighboring row is deleted outside QUI; the interval still refreshes the
    // complete loaded window because page one is outside stream coverage.
    refreshed = true
    await act(async () => {
      await vi.advanceTimersByTimeAsync(LOADED_WINDOW_POLL_INTERVAL_MS)
    })
    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    await flush()
    expect(pageCalls().filter(page => page === 0).length).toBe(page0CallsAfterLoad + 1)
    expect(pageCalls().filter(page => page === 1).length).toBe(page1CallsAfterLoad + 1)
    expect(result.current.torrents.find(t => t.hash === "c")?.state).toBe("uploading")
    expect(result.current.torrents.find(t => t.hash === "c")?.progress).toBe(1)
    expect(result.current.torrents.some(t => t.hash === "d")).toBe(false)
  })

  it("does not let an older loaded-window response overwrite a streamed completion", async () => {
    const oldPageZero = makeResponse({
      torrents: [
        makeTorrent({ hash: "a", state: "downloading", progress: 0.99, dlspeed: 1024 }),
        makeTorrent({ hash: "b" }),
      ],
      total: 4,
      hasMore: true,
    })
    const pageOne = makeResponse({
      torrents: [makeTorrent({ hash: "c" }), makeTorrent({ hash: "d" })],
      total: 4,
      hasMore: false,
    })
    let delayWindowRefresh = false
    let resolvePageZero: (response: TorrentResponse) => void = () => undefined

    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        if (delayWindowRefresh) {
          return new Promise<TorrentResponse>(resolve => {
            resolvePageZero = resolve
          })
        }
        return Promise.resolve(oldPageZero)
      }
      return Promise.resolve(pageOne)
    })

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const { result, rerender } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper(queryClient) }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["a", "b", "c", "d"])

    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    rerender()
    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: oldPageZero,
      })
    })
    await flush()

    delayWindowRefresh = true
    let refresh: Promise<void>
    act(() => {
      refresh = queryClient.refetchQueries({
        queryKey: ["torrents-list", 1],
        exact: false,
        type: "active",
      })
    })
    await flush()

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({
          torrents: [
            makeTorrent({ hash: "a", state: "uploading", progress: 1, dlspeed: 0 }),
            makeTorrent({ hash: "b" }),
          ],
          total: 4,
          hasMore: true,
        }),
      })
    })
    expect(result.current.torrents.find(torrent => torrent.hash === "a")?.state).toBe("uploading")

    act(() => {
      resolvePageZero(oldPageZero)
    })
    await act(async () => {
      await refresh!
    })
    await flush()

    const completed = result.current.torrents.find(torrent => torrent.hash === "a")
    expect(completed?.state).toBe("uploading")
    expect(completed?.progress).toBe(1)
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["a", "b", "c", "d"])

    const refreshCalls = mockedApi.getTorrents.mock.calls.slice(-2)
    expect(refreshCalls).toHaveLength(2)
    expect(refreshCalls.every(([, params]) => params.preferCached)).toBe(true)
  })

  it("keeps a row that reflows across a page boundary mid-fetch as a single entry", async () => {
    // Window pages are fetched in parallel against a live cache, so a row can slip
    // from one page's slice into another between requests and come back twice.
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
          total: 3,
          hasMore: true,
        }))
      }
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({ hash: "b" }), makeTorrent({ hash: "c" })],
        total: 3,
        hasMore: false,
      }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    expect(result.current.torrents.map(t => t.hash)).toEqual(["a", "b", "c"])
  })

  it("loadMore is a no-op while hasLoadedAll, and respects the 500ms throttle", async () => {
    // First page reports hasMore=false so hasLoadedAll becomes true.
    mockedApi.getTorrents.mockResolvedValueOnce(
      makeResponse({ torrents: [makeTorrent({ hash: "a" })], total: 1, hasMore: false })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()
    expect(result.current.hasLoadedAll).toBe(true)

    const callsAfterLoad = mockedApi.getTorrents.mock.calls.length

    act(() => {
      result.current.loadMore()
    })
    await flush()

    // hasLoadedAll guard: no new fetch.
    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(callsAfterLoad)
    expect(result.current.torrents).toHaveLength(1)
  })

  it("throttles a second loadMore within the 500ms window, then advances after it lapses", async () => {
    const page0 = [makeTorrent({ hash: "a" })]
    const page1 = [makeTorrent({ hash: "b" })]
    const page2 = [makeTorrent({ hash: "c" })]

    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({ torrents: page0, total: 3, hasMore: true }))
      }
      if (params.page === 1) {
        return Promise.resolve(makeResponse({ torrents: page1, total: 3, hasMore: true }))
      }
      return Promise.resolve(makeResponse({ torrents: page2, total: 3, hasMore: true }))
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    expect(result.current.torrents).toHaveLength(1)

    // Advance past the initial throttle window, then page in page 1. This call
    // records lastRequestTime via setState, which the next render observes.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents).toHaveLength(2)

    // Confirm the in-flight guards have cleared so the next early-return can only
    // be the 500ms throttle, not a residual isLoadingMore/isFetching guard
    // (loadMore returns early identically for all three, source lines 458-475).
    expect(result.current.isLoadingMore).toBe(false)
    expect(result.current.isFetching).toBe(false)

    // A second loadMore fired inside the 500ms window after the last request is
    // throttled: no page-2 fetch, list unchanged.
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(mockedApi.getTorrents).not.toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 2 }),
      expect.anything()
    )
    expect(result.current.torrents).toHaveLength(2)

    // Once the throttle window lapses, loadMore advances to page 2.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()
    expect(result.current.torrents).toHaveLength(3)
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 2 }),
      expect.any(AbortSignal)
    )
  })

  it("keeps pagination alive without replacing newer streamed rows or metadata", async () => {
    let pageOneSignal: AbortSignal | undefined
    let resolvePageOne: ((response: TorrentResponse) => void) | undefined
    const pageOneRequest = new Promise<TorrentResponse>(resolve => {
      resolvePageOne = resolve
    })

    mockedApi.getTorrents.mockImplementation((_instanceId, params, signal) => {
      if (params.page === 0) {
        return Promise.resolve(makeResponse({
          torrents: [makeTorrent({ hash: "a" })],
          total: 2,
          hasMore: true,
        }))
      }

      pageOneSignal = signal
      return pageOneRequest
    })

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    await flush()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    expect(pageOneSignal).toBeInstanceOf(AbortSignal)
    expect(result.current.isLoadingMore).toBe(true)

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({
          torrents: [makeTorrent({ hash: "a", name: "streamed-a" }), makeTorrent({ hash: "b", progress: 1, state: "uploading" })],
          total: 3,
          hasMore: true,
        }),
      })
    })
    await flush()

    expect(pageOneSignal?.aborted).toBe(false)

    act(() => {
      resolvePageOne?.(makeResponse({
        torrents: [makeTorrent({ hash: "b" })],
        total: 2,
        hasMore: false,
      }))
    })
    await flush()

    expect(result.current.isLoadingMore).toBe(false)
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["a", "b"])
    expect(result.current.totalCount).toBe(3)
    expect(result.current.hasLoadedAll).toBe(false)
    expect(result.current.torrents.find(torrent => torrent.hash === "b")?.progress).toBe(1)
    expect(result.current.torrents.find(torrent => torrent.hash === "b")?.state).toBe("uploading")
  })

  it("resets pagination state and re-fetches page 0 when filters change", async () => {
    const initial = [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })]
    const filtered = [makeTorrent({ hash: "z" })]

    mockedApi.getTorrents.mockImplementation((_instanceId, params) => {
      if (params.filters?.status?.includes("downloading")) {
        return Promise.resolve(makeResponse({ torrents: filtered, total: 1, hasMore: false }))
      }
      return Promise.resolve(makeResponse({ torrents: initial, total: 5, hasMore: true }))
    })

    const { result, rerender } = renderHook(
      ({ filters }: { filters?: TorrentFilters }) => useTorrentsList(1, { filters }),
      { wrapper: makeWrapper(), initialProps: { filters: undefined } as { filters?: TorrentFilters } }
    )

    await flush()
    expect(result.current.torrents).toHaveLength(2)
    expect(result.current.hasLoadedAll).toBe(false)

    rerender({ filters: { status: ["downloading"] } as TorrentFilters })

    await flush()

    expect(result.current.torrents).toEqual(
      expect.arrayContaining([expect.objectContaining({ hash: "z" })])
    )
    expect(result.current.torrents).toHaveLength(1)
    expect(result.current.hasLoadedAll).toBe(true)
  })

  it("routes cross-seed filters to the cross-instance endpoint and disables streaming", async () => {
    const crossTorrents = [
      { ...makeTorrent({ hash: "a" }), instanceId: 1, instanceName: "one" },
    ] as CrossInstanceTorrent[]
    mockedApi.getCrossInstanceTorrents.mockResolvedValue(
      makeResponse({
        isCrossInstance: true,
        crossInstanceTorrents: crossTorrents,
        total: 1,
        hasMore: false,
      })
    )

    const filters: TorrentFilters = {
      expr: "Hash == \"aaaa\" || Hash == \"bbbb\"",
    } as TorrentFilters

    const { result } = renderHook(() => useTorrentsList(1, { filters }), {
      wrapper: makeWrapper(),
    })

    await flush()

    expect(result.current.torrents).toHaveLength(1)
    expect(result.current.isCrossSeedFiltering).toBe(true)
    expect(result.current.isCrossInstanceEndpoint).toBe(true)
    expect(mockedApi.getCrossInstanceTorrents).toHaveBeenCalled()
    expect(mockedApi.getTorrents).not.toHaveBeenCalled()
    // Cross-seed forces streamParams null: no SSE subscription.
    expect(lastStreamParams).toBeNull()
    expect(lastStreamOptions?.enabled).toBe(false)
  })

  it("uses the cross-instance endpoint for the all-instances view with no capabilities", async () => {
    const crossTorrents = [
      { ...makeTorrent({ hash: "a" }), instanceId: 1, instanceName: "one" },
      { ...makeTorrent({ hash: "b" }), instanceId: 2, instanceName: "two" },
    ] as CrossInstanceTorrent[]
    mockedApi.getCrossInstanceTorrents.mockResolvedValue(
      makeResponse({
        isCrossInstance: true,
        cross_instance_torrents: crossTorrents,
        total: 2,
        hasMore: false,
      })
    )

    const { result } = renderHook(() => useTorrentsList(0), { wrapper: makeWrapper() })

    await flush()

    expect(result.current.torrents).toHaveLength(2)
    expect(result.current.isAllInstancesView).toBe(true)
    expect(result.current.isCrossInstanceEndpoint).toBe(true)
    expect(result.current.capabilities).toBeUndefined()
    expect(result.current.supportsTorrentCreation).toBe(false)
    expect(mockedApi.getCrossInstanceTorrents).toHaveBeenCalled()
  })

  it("merges a streamed snapshot and disables polling while connected", async () => {
    const streamedTorrents: Torrent[] = [
      makeTorrent({ hash: "s1", name: "streamed-one" }),
      makeTorrent({ hash: "s2", name: "streamed-two" }),
    ]

    // Stream is connected: REST page-0 query should be gated off.
    streamState = { ...DISCONNECTED, connected: true, initialized: true }

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    // Stream params exist for a single-instance view.
    expect(lastStreamParams).not.toBeNull()
    expect(typeof capturedOnMessage).toBe("function")

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: streamedTorrents, total: 2, hasMore: false }),
      })
    })

    await flush()

    expect(result.current.torrents).toHaveLength(2)
    expect(result.current.torrents.map(t => t.hash)).toEqual(["s1", "s2"])
    expect(result.current.totalCount).toBe(2)
    expect(result.current.isStreaming).toBe(true)
    // REST remains eligible until this listener owns a baseline. The init aborts
    // that in-flight fallback and takes ownership without allowing it to overwrite.
    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(1)
    expect(mockedApi.getTorrents.mock.calls[0]?.[2]?.aborted).toBe(true)
  })

  it("keeps the accepted stream cache successful and idle after cancelling REST", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let resolveRequest!: (response: TorrentResponse) => void
    mockedApi.getTorrents.mockReturnValue(new Promise(resolve => { resolveRequest = resolve }))
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper(queryClient) })
    await flush()
    const snapshot = makeResponse({ torrents: [makeTorrent({ hash: "stream" })], total: 1, hasMore: false })
    act(() => capturedOnMessage?.({ type: "init", data: snapshot }))
    await flush()
    // Even an API mock that ignores abort cannot overwrite the stream's cache.
    act(() => resolveRequest(makeResponse({ torrents: [makeTorrent({ hash: "rest" })] })))
    await flush()
    const query = queryClient.getQueryCache().findAll({ queryKey: ["torrents-list"] })[0]
    expect(query.state.data).toMatchObject({ torrents: [{ hash: "stream" }] })
    expect(query.state.status).toBe("success")
    expect(query.state.fetchStatus).toBe("idle")
    expect(query.state.error).toBeNull()
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["stream"])
  })

  it("keeps REST enabled when a shared stream has no baseline for this listener", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({
        torrents: [makeTorrent({ hash: "rest" })],
        total: 1,
        hasMore: false,
      })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })
    await flush()

    expect(mockedApi.getTorrents).toHaveBeenCalled()
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["rest"])
    expect(result.current.isStreaming).toBe(false)
  })

  it("uses REST fallback while retained stream data is stalled", async () => {
    streamState = {
      ...DISCONNECTED,
      connected: true,
      initialized: true,
    }
    let request = 0
    mockedApi.getTorrents.mockImplementation(() => {
      request += 1
      return Promise.resolve(makeResponse({
        torrents: [makeTorrent({
          hash: request === 1 ? "initial-rest" : "fallback-rest",
          name: request === 1 ? "initial REST" : "REST fallback",
        })],
        total: 1,
        hasMore: false,
      }))
    })

    const { result, rerender } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })
    await flush()

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({
          torrents: [makeTorrent({ hash: "stream", name: "retained stream row" })],
          total: 1,
          hasMore: false,
        }),
      })
    })
    await flush()
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["stream"])

    streamState = { ...streamState, dataStalled: true }
    rerender()
    await flush()

    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(2)
    expect(result.current.torrents.map(torrent => torrent.hash)).toEqual(["fallback-rest"])
    expect(result.current.isStreaming).toBe(false)
  })

  it("preserves last stream counts when a connected stream snapshot omits counts", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const counts = makeCounts()

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false, counts }),
      })
    })
    await flush()
    expect(result.current.counts).toBe(counts)

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false }),
      })
    })
    await flush()

    expect(result.current.counts).toBe(counts)
    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(1)
    expect(mockedApi.getTorrents.mock.calls[0]?.[2]?.aborted).toBe(true)
  })

  it("keeps explicit zero stream counts authoritative over preserved counts", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const previousCounts = makeCounts()
    const zeroCounts = makeCounts({
      status: { all: 0 },
      trackers: {},
      total: 0,
    })

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false, counts: previousCounts }),
      })
    })
    await flush()

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [], total: 0, hasMore: false, counts: zeroCounts }),
      })
    })
    await flush()

    // toEqual, not toBe: the stream write now flows through query-cache
    // structural sharing, which may rebuild the counts container. The contract
    // is the zero VALUES winning over the preserved fallback, not the identity
    // of the frame's object.
    expect(result.current.counts).toEqual(zeroCounts)
  })

  it("does not preserve stream counts across filter scope changes", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const counts = makeCounts()
    const downloadingFilter = { status: ["downloading"] } as TorrentFilters

    const { result, rerender } = renderHook(
      ({ filters }: { filters?: TorrentFilters }) => useTorrentsList(1, { filters }),
      { wrapper: makeWrapper(), initialProps: { filters: undefined } as { filters?: TorrentFilters } }
    )

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false, counts }),
      })
    })
    await flush()
    expect(result.current.counts).toBe(counts)

    rerender({ filters: downloadingFilter })
    await flush()
    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s2" })], total: 1, hasMore: false }),
      })
    })
    await flush()

    expect(result.current.counts).toBeUndefined()
  })

  it("ignores full stream frames from a previous scope after search changes", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }

    const { result, rerender } = renderHook(
      ({ search }: { search?: string }) => useTorrentsList(1, { pollingEnabled: false, search }),
      { wrapper: makeWrapper(), initialProps: { search: undefined } as { search?: string } }
    )

    const previousOnMessage = capturedOnMessage
    expect(typeof previousOnMessage).toBe("function")

    act(() => {
      previousOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "old" })], total: 1, hasMore: false }),
      })
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["old"])

    rerender({ search: "new scope" })
    await flush()
    expect(result.current.torrents).toEqual([])

    act(() => {
      previousOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "stale" })], total: 1, hasMore: false }),
      })
    })
    await flush()
    expect(result.current.torrents).toEqual([])
    expect(result.current.totalCount).toBe(0)

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "fresh" })], total: 1, hasMore: false }),
      })
    })
    await flush()
    expect(result.current.torrents.map(t => t.hash)).toEqual(["fresh"])
    expect(result.current.totalCount).toBe(1)
  })

  it("keeps the committed stream callback active when a scope render is abandoned", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const pending = new Promise<never>(() => undefined)
    let committedList: ReturnType<typeof useTorrentsList> | undefined

    function Probe({ search, suspend }: { search?: string; suspend?: boolean }) {
      const list = useTorrentsList(1, { pollingEnabled: false, search })

      useEffect(() => {
        committedList = list
      }, [list])

      if (suspend) {
        throw pending
      }

      return null
    }

    const view = render(
      <Suspense fallback={null}>
        <Probe search={undefined} />
      </Suspense>,
      { wrapper: makeWrapper() }
    )

    const committedOnMessage = capturedOnMessage
    expect(typeof committedOnMessage).toBe("function")

    act(() => {
      committedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "old" })], total: 1, hasMore: false }),
      })
    })
    await flush()
    expect(committedList?.torrents.map(t => t.hash)).toEqual(["old"])

    act(() => {
      startTransition(() => {
        view.rerender(
          <Suspense fallback={null}>
            <Probe search="abandoned" suspend />
          </Suspense>
        )
      })
    })
    await flush()

    act(() => {
      committedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "still-active" })], total: 1, hasMore: false }),
      })
    })
    await flush()

    expect(committedList?.torrents.map(t => t.hash)).toEqual(["still-active"])
    expect(committedList?.totalCount).toBe(1)
  })

  it("does not publish counts from an abandoned stream render", async () => {
    streamState = { ...DISCONNECTED, connected: true, initialized: true }
    const committedCounts = makeCounts({ status: { all: 1 }, total: 1 })
    const abandonedCounts = makeCounts({ status: { all: 2 }, total: 2 })
    const pending = new Promise<never>(() => undefined)
    let suspendAfterHook = false
    let committedList: ReturnType<typeof useTorrentsList> | undefined

    function Probe() {
      const list = useTorrentsList(1)

      useEffect(() => {
        committedList = list
      }, [list])

      if (suspendAfterHook) {
        throw pending
      }

      return null
    }

    render(
      <Suspense fallback={null}>
        <Probe />
      </Suspense>,
      { wrapper: makeWrapper() }
    )

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false, counts: committedCounts }),
      })
    })
    await flush()
    expect(committedList?.counts).toBe(committedCounts)

    suspendAfterHook = true
    act(() => {
      startTransition(() => {
        capturedOnMessage?.({
          type: "update",
          data: makeResponse({ torrents: [makeTorrent({ hash: "s2" })], total: 2, hasMore: false, counts: abandonedCounts }),
        })
      })
    })
    await flush()
    expect(committedList?.counts).toBe(committedCounts)

    suspendAfterHook = false
    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false }),
      })
    })
    await flush()

    expect(committedList?.counts).toBe(committedCounts)
  })

  it("merges a streamed cross-instance snapshot for the all-instances view and survives REST pagination (#1983)", async () => {
    // All-instances view with a connected stream routes through the cross-instance
    // STREAM merge branch (mergeStreamedCrossInstanceFirstPage), keyed on
    // instanceId+hash. Distinct rows that share a hash across instances stay separate.
    streamState = { ...DISCONNECTED, connected: true, initialized: true }

    // page-0 stream rows: same hash "x" on two instances are two distinct rows.
    const page0Rows = [
      { ...makeTorrent({ hash: "x", name: "x-on-1" }), instanceId: 1, instanceName: "one" },
      { ...makeTorrent({ hash: "x", name: "x-on-2" }), instanceId: 2, instanceName: "two" },
    ] as CrossInstanceTorrent[]
    const aggregateCounts = makeCounts({ status: { all: 2 }, total: 2 })

    // A later REST page the user paginates in; must survive the next page-0 snapshot.
    const laterPageRow = [
      { ...makeTorrent({ hash: "y", name: "y-on-1" }), instanceId: 1, instanceName: "one" },
    ] as CrossInstanceTorrent[]

    mockedApi.getCrossInstanceTorrents.mockImplementation((params) => {
      if (params.page === 0) {
        return Promise.resolve(
          makeResponse({ isCrossInstance: true, cross_instance_torrents: page0Rows, total: 10, hasMore: true })
        )
      }
      return Promise.resolve(
        makeResponse({ isCrossInstance: true, cross_instance_torrents: laterPageRow, total: 10, hasMore: true })
      )
    })

    const { result } = renderHook(
      () => useTorrentsList(0, { pollingEnabled: false }),
      { wrapper: makeWrapper() }
    )

    // Aggregated view subscribes to the cross-instance stream.
    expect(lastStreamParams).not.toBeNull()
    expect(typeof capturedOnMessage).toBe("function")

    // First streamed snapshot delivers page 0. snake_case rows are normalized.
    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({
          isCrossInstance: true,
          cross_instance_torrents: page0Rows,
          total: 10,
          hasMore: true,
          counts: aggregateCounts,
        }),
      })
    })
    await flush()

    // (a) rows merge in order, both instanceId+hash rows kept distinct.
    expect(result.current.torrents.map(t => (t as CrossInstanceTorrent).instanceId)).toEqual([1, 2])
    expect(result.current.torrents).toHaveLength(2)
    // (b) totalCount comes from the streamed data.total.
    expect(result.current.totalCount).toBe(10)
    expect(result.current.counts).toBe(aggregateCounts)
    expect(result.current.isStreaming).toBe(true)
    expect(result.current.isCrossInstanceEndpoint).toBe(true)

    // Paginate in a later REST page (page 0 REST is gated off while connected, but
    // page > 0 still fetches). This appends the trailing row to the displayed list.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    act(() => {
      result.current.loadMore()
    })
    await flush()

    expect(result.current.torrents).toHaveLength(3)
    expect(result.current.torrents.map(t => t.hash)).toEqual(["x", "x", "y"])
    expect(mockedApi.getCrossInstanceTorrents).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1 }),
      expect.any(AbortSignal)
    )

    // (c) A fresh page-0 stream snapshot must NOT wipe the paginated-in later row:
    // the cross-instance merge keeps trailing pages alive (issue #1983 regression).
    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({
          isCrossInstance: true,
          cross_instance_torrents: page0Rows,
          total: 10,
          hasMore: true,
        }),
      })
    })
    await flush()

    expect(result.current.torrents).toHaveLength(3)
    expect(result.current.torrents.map(t => t.hash)).toEqual(["x", "x", "y"])
    expect(result.current.counts).toBe(aggregateCounts)
  })

  it("polls page 0 on the configured interval while the stream is disconnected", async () => {
    // Disconnected stream + polling enabled: the REST page-0 query keeps polling.
    streamState = { ...DISCONNECTED, error: "client:disconnected" }
    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({ torrents: [makeTorrent({ hash: "a" })], total: 1, hasMore: false })
    )

    renderHook(() => useTorrentsList(1, { pollingEnabled: true }), {
      wrapper: makeWrapper(),
    })

    await flush()
    const initialCalls = mockedApi.getTorrents.mock.calls.length
    expect(initialCalls).toBeGreaterThanOrEqual(1)

    // Advance past the single-instance poll interval: a second page-0 fetch fires.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(TORRENT_STREAM_POLL_INTERVAL_MS + 50)
    })
    await flush()

    const polledCalls = mockedApi.getTorrents.mock.calls.length
    expect(polledCalls).toBeGreaterThan(initialCalls)
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 0 }),
      expect.any(AbortSignal)
    )
  })

  it("does not poll page 0 once the stream is connected", async () => {
    // Connected stream gates the page-0 REST query off entirely; no poll ever fires.
    streamState = { ...DISCONNECTED, connected: true, initialized: true }

    const { result } = renderHook(
      () => useTorrentsList(1, { pollingEnabled: true }),
      { wrapper: makeWrapper() }
    )

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({ torrents: [makeTorrent({ hash: "s1" })], total: 1, hasMore: false }),
      })
    })
    await flush()
    expect(result.current.torrents).toHaveLength(1)
    const callsAfterInit = mockedApi.getTorrents.mock.calls.length

    // Advancing well past the poll interval must not produce any getTorrents call.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(TORRENT_STREAM_POLL_INTERVAL_MS * 2)
    })
    await flush()

    expect(mockedApi.getTorrents).toHaveBeenCalledTimes(callsAfterInit)
  })

  it("ERROR PATH: a rejected getTorrents settles loading false and keeps the list empty", async () => {
    mockedApi.getTorrents.mockRejectedValue(new Error("boom"))
    // Stream errored too, so the hook is not stuck in its "stream still connecting"
    // initial-loading state and the REST failure is the only loading signal.
    streamState = { ...DISCONNECTED, error: "client:disconnected" }

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    expect(mockedApi.getTorrents).toHaveBeenCalled()
    expect(result.current.isLoading).toBe(false)
    expect(result.current.torrents).toEqual([])
  })

  it("ERROR PATH: stream client:disconnected keeps the REST page-0 query running", async () => {
    // Stream params are set, but the stream reports an error and is not connected.
    // Regression guard: gating on error alone would disable REST and never load.
    streamState = { ...DISCONNECTED, connected: false, error: "client:disconnected" }

    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({ torrents: [makeTorrent({ hash: "r" })], total: 1, hasMore: false })
    )

    const { result } = renderHook(() => useTorrentsList(1), { wrapper: makeWrapper() })

    await flush()

    expect(result.current.torrents).toHaveLength(1)
    expect(lastStreamParams).not.toBeNull()
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ page: 0 }),
      expect.any(AbortSignal)
    )
    expect(result.current.torrents[0].hash).toBe("r")
  })
})

// Preserves the stream visibility-gating coverage from #1994: the single-instance
// list subscription (a heavy limit:300 snapshot) is paused once the tab has been
// hidden past the delay, and resumes immediately on refocus.
describe("useTorrentsList background stream gating", () => {
  it("keeps the list stream enabled while the tab is visible", () => {
    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper() })

    expect(lastStreamOptions?.enabled).toBe(true)
  })

  it("pauses the list stream once the tab has been hidden past the delay", () => {
    const { rerender } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper() })

    expect(lastStreamOptions?.enabled).toBe(true)

    setHidden(true)
    act(() => {
      vi.advanceTimersByTime(STREAM_HIDDEN_PAUSE_DELAY_MS)
    })
    rerender()

    expect(lastStreamOptions?.enabled).toBe(false)
  })

  it("resumes the list stream immediately when the tab becomes visible again", () => {
    const { rerender } = renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper() })

    setHidden(true)
    act(() => {
      vi.advanceTimersByTime(STREAM_HIDDEN_PAUSE_DELAY_MS)
    })
    rerender()
    expect(lastStreamOptions?.enabled).toBe(false)

    setHidden(false)
    rerender()

    expect(lastStreamOptions?.enabled).toBe(true)
  })

  it("preserves preference caches when a regular stream frame omits preferences", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    queryClient.setQueryData(["instance-preferences", 1], makePreferences({ listen_port: 6881, use_subcategories: true }))
    queryClient.setQueryData(["instance-metadata", 1], {
      categories: {},
      tags: [],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: true }),
    })

    const { result } = renderHook(() => ({
      list: useTorrentsList(1, { pollingEnabled: false }),
      preferences: useInstancePreferences(1, { fetchIfMissing: false }),
    }), { wrapper: makeWrapper(queryClient) })

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: {
          torrents: [],
          total: 0,
          hasMore: false,
          categories: {},
          tags: [],
        },
      })
    })

    expect(queryClient.getQueryData<{ preferences?: unknown }>(["instance-metadata", 1])?.preferences).toEqual({
      listen_port: 6881,
      use_subcategories: true,
    })
    expect(queryClient.getQueryData(["instance-preferences", 1])).toEqual({
      listen_port: 6881,
      use_subcategories: true,
    })
    expect(result.current.preferences.preferences?.use_subcategories).toBe(true)
  })

  it("preserves preference caches when a regular delta frame omits preferences", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    const preferences = makePreferences({ listen_port: 6881, use_subcategories: true })
    const torrent = makeTorrent({ hash: "a" })

    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })

    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({
          torrents: [torrent],
          total: 1,
          hasMore: false,
          preferences,
        }),
      })
    })

    act(() => {
      capturedOnMessage?.({
        type: "delta",
        data: makeResponse({ torrents: [], total: 1, hasMore: false }),
      })
    })

    expect(queryClient.getQueryData<{ preferences?: unknown }>(["instance-metadata", 1])?.preferences).toBe(preferences)
    expect(queryClient.getQueryData(["instance-preferences", 1])).toBe(preferences)
  })

  it("updates preference caches when a regular stream frame includes preferences", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    queryClient.setQueryData(["instance-preferences", 1], makePreferences({ listen_port: 6881, use_subcategories: false }))
    queryClient.setQueryData(["instance-metadata", 1], {
      categories: {},
      tags: [],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: false }),
    })

    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: {
          torrents: [],
          total: 0,
          hasMore: false,
          categories: {},
          tags: [],
          preferences: { listen_port: 51413, use_subcategories: true } as unknown as TorrentResponse["preferences"],
        },
      })
    })

    expect(queryClient.getQueryData<{ preferences?: unknown }>(["instance-metadata", 1])?.preferences).toEqual({
      listen_port: 51413,
      use_subcategories: true,
    })
    expect(queryClient.getQueryData(["instance-preferences", 1])).toEqual({
      listen_port: 51413,
      use_subcategories: true,
    })
  })

  it("clears preference caches when a regular stream frame explicitly nulls preferences", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    queryClient.setQueryData(["instance-preferences", 1], makePreferences({ listen_port: 6881, use_subcategories: true }))
    queryClient.setQueryData(["instance-metadata", 1], {
      categories: {},
      tags: [],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: true }),
    })

    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: {
          torrents: [],
          total: 0,
          hasMore: false,
          categories: {},
          tags: [],
          preferences: null as unknown as TorrentResponse["preferences"],
        },
      })
    })

    expect(queryClient.getQueryData<{ preferences?: unknown }>(["instance-metadata", 1])?.preferences).toBeUndefined()
    expect(queryClient.getQueryData(["instance-preferences", 1])).toBeUndefined()
  })

  it("clears stale preference caches when a fresh REST response explicitly nulls preferences", async () => {
    streamState = { ...DISCONNECTED }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    queryClient.setQueryData(["instance-preferences", 1], makePreferences({ listen_port: 6881, use_subcategories: true }))
    queryClient.setQueryData(["instance-metadata", 1], {
      categories: {},
      tags: [],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: true }),
    })
    mockedApi.getTorrents.mockResolvedValue(makeResponse({
      categories: {},
      tags: [],
      preferences: null,
    }))

    renderHook(() => useTorrentsList(1, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })
    await flush()

    expect(queryClient.getQueryData<{ preferences?: unknown }>(["instance-metadata", 1])?.preferences).toBeUndefined()
    expect(queryClient.getQueryData(["instance-preferences", 1])).toBeUndefined()
  })

  it("leaves cross-instance metadata untouched when preferences are omitted", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    queryClient.setQueryData(["instance-metadata", 0], {
      categories: { old: { name: "old", savePath: "" } },
      tags: ["old"],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: true }),
    })

    renderHook(() => useTorrentsList(0, { pollingEnabled: false }), { wrapper: makeWrapper(queryClient) })

    act(() => {
      capturedOnMessage?.({
        type: "update",
        data: makeResponse({
          isCrossInstance: true,
          cross_instance_torrents: [],
          total: 0,
          hasMore: false,
          categories: { new: { name: "new", savePath: "" } },
          tags: ["new"],
        }),
      })
    })

    expect(queryClient.getQueryData(["instance-metadata", 0])).toEqual({
      categories: { old: { name: "old", savePath: "" } },
      tags: ["old"],
      preferences: makePreferences({ listen_port: 6881, use_subcategories: true }),
    })
  })

  it("keeps one object identity for an unchanged row across REST, stream frames, and cache echoes", async () => {
    // Row memoization compares torrents by reference, so an unchanged row must
    // keep ONE identity across every write path: the REST fetch, each stream
    // frame (whose parsed objects are equal but not identical), and the
    // processing effect that re-applies the query-cache echo of each stream
    // write. This captures every render, not just the settled end state — the
    // regression mode is a transient flip to a second lineage and back within
    // one tick.
    const seenIdentities: Torrent[] = []

    mockedApi.getTorrents.mockResolvedValue(
      makeResponse({
        torrents: [makeTorrent({ hash: "a", name: "alpha" }), makeTorrent({ hash: "b", name: "beta" })],
        total: 2,
        hasMore: false,
      })
    )

    const { result } = renderHook(() => {
      const list = useTorrentsList(1, { pollingEnabled: false })
      const rowA = list.torrents.find(t => t.hash === "a")
      if (rowA) {
        seenIdentities.push(rowA)
      }
      return list
    }, { wrapper: makeWrapper() })

    await flush()
    expect(result.current.torrents).toHaveLength(2)

    // Full stream frame: same values as the REST rows, freshly parsed objects.
    act(() => {
      capturedOnMessage?.({
        type: "init",
        data: makeResponse({
          torrents: [makeTorrent({ hash: "a", name: "alpha" }), makeTorrent({ hash: "b", name: "beta" })],
          total: 2,
          hasMore: false,
        }),
      })
    })
    await flush()

    // Two delta ticks that change only row b; row a rides along untouched.
    for (const dlspeed of [100, 200]) {
      act(() => {
        capturedOnMessage?.({
          type: "delta",
          data: makeResponse({
            torrents: [makeTorrent({ hash: "b", name: "beta", dlspeed })],
            total: 2,
            hasMore: false,
          }),
        })
      })
      await flush()
    }

    const distinctIdentities = new Set(seenIdentities)
    expect(seenIdentities.length).toBeGreaterThan(0)
    expect(distinctIdentities.size).toBe(1)

    // And the changed row did update.
    expect(result.current.torrents.find(t => t.hash === "b")?.dlspeed).toBe(200)
  })
})
