/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { focusManager, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import { useMemo } from "react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import type { ActionTarget } from "@/lib/torrent-selection"
import { makeTorrent } from "@/test/mockTorrent"
import type { LocalCrossSeedMatch, Torrent, TorrentFilters, TorrentResponse } from "@/types"

vi.mock("@/lib/api", () => ({
  api: {
    getTorrents: vi.fn(),
    getCrossInstanceTorrents: vi.fn(),
    getDeleteCrossSeeds: vi.fn(),
    addCrossSeedBlocklist: vi.fn(),
  },
}))

vi.mock("@/hooks/useInstances", () => ({
  useInstances: vi.fn(),
}))

vi.mock("@/hooks/useInstanceCapabilities", () => ({
  useInstanceCapabilities: vi.fn(),
}))

// The same stream stand-in as the list hook tests: the REST path runs, and the
// captured options show whether the list stream is open.
let streamState: StreamState
let lastStreamOptions: { enabled?: boolean } | undefined
let hidden = false
let originalHiddenDescriptor: PropertyDescriptor | undefined

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: vi.fn((_params: unknown, options: { enabled?: boolean } = {}) => {
    lastStreamOptions = options
    return streamState
  }),
}))

import { api } from "@/lib/api"
import { useInstances } from "@/hooks/useInstances"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useTorrentListController, useTorrentListCrossSeed } from "@/hooks/useTorrentListController"
import { STREAM_HIDDEN_PAUSE_DELAY_MS, TORRENT_STREAM_POLL_INTERVAL_MS } from "@/hooks/useTorrentsList"

const mockedApi = vi.mocked(api, true)

const FILTERS: TorrentFilters = {
  status: [],
  excludeStatus: [],
  categories: [],
  excludeCategories: [],
  tags: [],
  excludeTags: [],
  trackers: [],
  excludeTrackers: [],
}

function makeResponse(overrides: Partial<TorrentResponse> = {}): TorrentResponse {
  return { torrents: [], total: 0, hasMore: false, ...overrides }
}

function makeWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

async function flush(times = 8) {
  for (let i = 0; i < times; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
  }
}

// The browser hides the tab: document.hidden changes, and React Query's focus
// manager follows the visibility of the window.
function setHidden(next: boolean) {
  hidden = next
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"))
  })
  act(() => {
    focusManager.setFocused(!next)
  })
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

interface LayoutProps {
  filters?: TorrentFilters
  columnFiltersExpr?: string | null
  search?: string
  sort?: string
  /** Builds the Action target from the loaded rows, as a Layout does. */
  target?: (rows: Torrent[]) => ActionTarget
  blockCrossSeeds?: boolean
}

const tickAll = (rows: Torrent[]): ActionTarget => ({
  kind: "ticks",
  hashes: rows.map(row => row.hash),
  count: rows.length,
  previewTorrents: rows,
})

// Acts like a Layout: load the list, build the Action target from its rows, and read the cross-seed state.
function useLayout({ filters = FILTERS, columnFiltersExpr, search = "", sort = "added_on", target = tickAll, blockCrossSeeds = false }: LayoutProps) {
  const list = useTorrentListController({
    instanceId: 1,
    filters,
    columnFiltersExpr,
    search,
    sort,
    order: "desc",
  })
  const actionTarget = useMemo(() => target(list.torrents), [target, list.torrents])
  const crossSeed = useTorrentListCrossSeed({ instanceId: 1, actionTarget, blockCrossSeeds })
  return { list, crossSeed }
}

function renderLayout(initialProps: LayoutProps = {}) {
  return renderHook((props: LayoutProps) => useLayout(props), { initialProps, wrapper: makeWrapper() })
}

function crossSeedMatch(overrides: Partial<LocalCrossSeedMatch>): LocalCrossSeedMatch {
  return {
    instanceId: 1,
    instanceName: "qb",
    hash: "match",
    name: "Example.Release.2024.1080p-GRP",
    size: 1,
    progress: 1,
    savePath: "/data",
    contentPath: "/data/x",
    category: "",
    tags: "",
    state: "uploading",
    tracker: "",
    matchType: "content_path",
    ...overrides,
  } as LocalCrossSeedMatch
}

beforeEach(() => {
  vi.useFakeTimers()
  streamState = { connected: false, initialized: false, dataStalled: false, error: null, retrying: false, retryAttempt: 0 }
  lastStreamOptions = undefined
  hidden = false
  originalHiddenDescriptor = Object.getOwnPropertyDescriptor(document, "hidden")
  Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden })

  vi.mocked(useInstances).mockReturnValue({
    instances: [{ id: 1, isActive: true }],
  } as unknown as ReturnType<typeof useInstances>)
  vi.mocked(useInstanceCapabilities).mockReturnValue({
    data: { supportsSubcategories: true, supportsTorrentCreation: true },
  } as unknown as ReturnType<typeof useInstanceCapabilities>)

  mockedApi.getTorrents.mockResolvedValue(makeResponse())
  mockedApi.getCrossInstanceTorrents.mockResolvedValue(makeResponse({ isCrossInstance: true }))
  mockedApi.getDeleteCrossSeeds.mockResolvedValue({ matches: [], uncheckedInstances: [] })
  mockedApi.addCrossSeedBlocklist.mockResolvedValue({} as Awaited<ReturnType<typeof api.addCrossSeedBlocklist>>)
})

afterEach(() => {
  cleanup()
  focusManager.setFocused(undefined)
  vi.useRealTimers()
  vi.clearAllMocks()
  if (originalHiddenDescriptor) {
    Object.defineProperty(document, "hidden", originalHiddenDescriptor)
  } else {
    Reflect.deleteProperty(document, "hidden")
  }
})

describe("useTorrentListController — list", () => {
  it("returns the rows, the total and the counts of the response", async () => {
    const counts = { status: { all: 2 }, categories: {}, tags: {}, trackers: {}, total: 2 }
    mockedApi.getTorrents.mockResolvedValue(makeResponse({
      torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })],
      total: 2,
      counts,
    }))

    const { result } = renderLayout()
    await flush()

    expect(result.current.list.torrents.map(row => row.hash)).toEqual(["a", "b"])
    expect(result.current.list.totalCount).toBe(2)
    expect(result.current.list.counts).toEqual(counts)
  })

  const lastExpr = () => mockedApi.getTorrents.mock.lastCall?.[1].filters?.expr

  it("sends the sidebar expression alone when the Layout passes no column filters", async () => {
    renderLayout({ filters: { ...FILTERS, expr: "state == \"x\"" } })
    await flush()
    expect(lastExpr()).toBe("state == \"x\"")
  })

  it("joins the column filters and the sidebar expression", async () => {
    renderLayout({ filters: { ...FILTERS, expr: "state == \"x\"" }, columnFiltersExpr: "AddedOn > 1704067200" })
    await flush()
    expect(lastExpr()).toBe("(AddedOn > 1704067200) && (state == \"x\")")
  })

  // regression: #1925
  it("leaves the column filters out of a cross-seed request", async () => {
    const crossSeedExpr = "Hash == \"abc\" || Hash == \"def\""
    renderLayout({ filters: { ...FILTERS, expr: crossSeedExpr }, columnFiltersExpr: "AddedOn > 1704067200" })
    await flush()
    expect(mockedApi.getCrossInstanceTorrents.mock.lastCall?.[0].filters?.expr).toBe(crossSeedExpr)
  })
})

describe("useTorrentListController — load more", () => {
  const rows = (count: number, from = 0) => Array.from({ length: count }, (_, i) => makeTorrent({ hash: `h${from + i}` }))

  // The server holds 350 rows: a first page of 300 and a second page of 50.
  beforeEach(() => {
    mockedApi.getTorrents.mockImplementation(async (_instanceId, params) => params.page === 0
      ? makeResponse({ torrents: rows(300), total: 350, hasMore: true })
      : makeResponse({ torrents: rows(50, 300), total: 350, hasMore: false }))
  })

  const pagesRequested = () => mockedApi.getTorrents.mock.calls.map(([, params]) => params.page)

  async function scrollToEnd(result: { current: ReturnType<typeof useLayout> }) {
    while (result.current.list.loadedRows < result.current.list.torrents.length) {
      act(() => result.current.list.reportNearEnd())
    }
    await flush()
  }

  it("shows the next rows from memory, then loads the next server page, near the end", async () => {
    const { result } = renderLayout()
    await flush()
    expect(result.current.list.loadedRows).toBe(100)
    expect(result.current.list.hasMoreRows).toBe(true)

    act(() => result.current.list.reportNearEnd())
    expect(result.current.list.loadedRows).toBe(200)
    expect(pagesRequested()).not.toContain(1)

    act(() => result.current.list.reportNearEnd())
    expect(result.current.list.loadedRows).toBe(300)

    act(() => result.current.list.reportNearEnd())
    await flush()
    expect(pagesRequested()).toContain(1)
    expect(result.current.list.torrents).toHaveLength(350)
    expect(result.current.list.loadedRows).toBe(350)
    expect(result.current.list.hasMoreRows).toBe(false)
  })

  it("starts again at the first rows when the user changes a filter, the search or the sort", async () => {
    const { result, rerender } = renderLayout()
    await flush()

    await scrollToEnd(result)
    expect(result.current.list.loadedRows).toBe(300)
    rerender({ filters: { ...FILTERS, status: ["downloading"] } })
    await flush()
    expect(result.current.list.loadedRows).toBe(100)

    await scrollToEnd(result)
    rerender({ filters: { ...FILTERS, status: ["downloading"] }, search: "linux" })
    await flush()
    expect(result.current.list.loadedRows).toBe(100)

    await scrollToEnd(result)
    rerender({ filters: { ...FILTERS, status: ["downloading"] }, search: "linux", sort: "name" })
    await flush()
    expect(result.current.list.loadedRows).toBe(100)
  })

  // React Query answers from its cache, so the list never goes empty between the two filters.
  it("starts again at the first rows when the user goes back to a filter with a cached response", async () => {
    const downloading = { ...FILTERS, status: ["downloading"] }
    const { result, rerender } = renderLayout()
    await flush()
    rerender({ filters: downloading })
    await flush()
    await scrollToEnd(result)
    expect(result.current.list.loadedRows).toBe(300)

    rerender({ filters: FILTERS })
    await flush()
    expect(result.current.list.loadedRows).toBe(100)
  })

  it("never reports more loaded rows than the list holds while a filter change empties the list", async () => {
    const seen: Array<{ loaded: number; rows: number }> = []
    const { result, rerender } = renderHook((props: LayoutProps) => {
      const layout = useLayout(props)
      seen.push({ loaded: layout.list.loadedRows, rows: layout.list.torrents.length })
      return layout
    }, { initialProps: {} as LayoutProps, wrapper: makeWrapper() })
    await flush()
    await scrollToEnd(result)

    rerender({ filters: { ...FILTERS, status: ["downloading"] } })
    await flush()
    expect(seen.filter(render => render.loaded > render.rows)).toEqual([])
  })

  it("keeps the loaded rows when a new filters object has the same contents", async () => {
    const { result, rerender } = renderLayout()
    await flush()
    await scrollToEnd(result)

    rerender({ filters: { ...FILTERS } })
    await flush()
    expect(result.current.list.loadedRows).toBe(300)
  })

  it("keeps the loaded rows when a data update changes the length", async () => {
    const { result } = renderLayout()
    await flush()
    await scrollToEnd(result)

    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: rows(301), total: 351, hasMore: true }))
    await advance(TORRENT_STREAM_POLL_INTERVAL_MS + 50)
    await flush()
    expect(result.current.list.torrents).toHaveLength(301)
    expect(result.current.list.loadedRows).toBe(300)
  })
})

describe("useTorrentListController — hidden tab", () => {
  it("keeps the stream open and stops polling while the tab is hidden for less than the pause delay", async () => {
    renderLayout()
    await flush()
    expect(lastStreamOptions?.enabled).toBe(true)

    setHidden(true)
    const callsWhenHidden = mockedApi.getTorrents.mock.calls.length
    await advance(10_000)

    expect(lastStreamOptions?.enabled).toBe(true)
    expect(mockedApi.getTorrents.mock.calls.length).toBe(callsWhenHidden)
  })

  it("closes the stream once the tab is hidden past the pause delay", async () => {
    renderLayout()
    await flush()

    setHidden(true)
    await advance(STREAM_HIDDEN_PAUSE_DELAY_MS)

    expect(lastStreamOptions?.enabled).toBe(false)
  })

  it("opens the stream, refetches and polls again when the tab shows again", async () => {
    renderLayout()
    await flush()

    setHidden(true)
    await advance(STREAM_HIDDEN_PAUSE_DELAY_MS)
    const callsWhenShown = mockedApi.getTorrents.mock.calls.length

    setHidden(false)
    await flush()
    expect(lastStreamOptions?.enabled).toBe(true)
    expect(mockedApi.getTorrents.mock.calls.length).toBe(callsWhenShown + 1)

    await advance(TORRENT_STREAM_POLL_INTERVAL_MS)
    expect(mockedApi.getTorrents.mock.calls.length).toBe(callsWhenShown + 2)
  })
})

describe("useTorrentListCrossSeed", () => {
  it("flags the cross-seed tag of a ticked row, and blocks only when the user asks", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: [makeTorrent({ hash: "a", tags: "cross-seed" })], total: 1 }))

    const { result, rerender } = renderLayout()
    await flush()
    expect(result.current.crossSeed.hasCrossSeedTag).toBe(true)
    expect(result.current.crossSeed.shouldBlockCrossSeeds).toBe(false)

    rerender({ blockCrossSeeds: true })
    expect(result.current.crossSeed.shouldBlockCrossSeeds).toBe(true)
  })

  it("does not flag or block a target without the tag", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: [makeTorrent({ hash: "a", tags: "linux" })], total: 1 }))

    const { result } = renderLayout({ blockCrossSeeds: true })
    await flush()
    expect(result.current.crossSeed.hasCrossSeedTag).toBe(false)
    expect(result.current.crossSeed.shouldBlockCrossSeeds).toBe(false)
  })

  it("checks the ticked rows and flags the tag of an affected cross-seed", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: [makeTorrent({ hash: "a", tags: "linux" })], total: 1 }))
    mockedApi.getDeleteCrossSeeds.mockResolvedValue({ matches: [crossSeedMatch({ tags: "cross-seed" })], uncheckedInstances: [] })

    const { result } = renderLayout()
    await flush()
    act(() => {
      result.current.crossSeed.crossSeedWarning.search()
    })
    await flush()

    expect(mockedApi.getDeleteCrossSeeds).toHaveBeenCalledWith(1, { targets: [{ instanceId: 1, hash: "a" }] }, expect.any(AbortSignal))
    expect(result.current.crossSeed.crossSeedWarning.hasWarning).toBe(true)
    expect(result.current.crossSeed.hasCrossSeedTag).toBe(true)
  })

  it("checks the Select all scope, not only the loaded rows", async () => {
    const selectAll = { filters: FILTERS, search: "linux", excludeHashes: ["b"] }
    const target = (): ActionTarget => ({ kind: "selectAll", selectAll, count: 40, previewTorrents: [] })

    const { result } = renderLayout({ target })
    await flush()
    act(() => {
      result.current.crossSeed.crossSeedWarning.search()
    })
    await flush()

    expect(mockedApi.getDeleteCrossSeeds).toHaveBeenCalledWith(1, { selectAll: true, ...selectAll }, expect.any(AbortSignal))
  })

  it("adds each hash to the blocklist", async () => {
    const { result } = renderLayout()
    await flush()
    await act(async () => {
      await result.current.crossSeed.blockCrossSeedHashes(["abc"])
    })

    expect(mockedApi.addCrossSeedBlocklist).toHaveBeenCalledWith({ instanceId: 1, infoHash: "abc" })
  })
})
