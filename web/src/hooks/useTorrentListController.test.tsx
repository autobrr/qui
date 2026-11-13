/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { focusManager, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import { makeTorrent } from "@/test/mockTorrent"
import type { TorrentFilters, TorrentResponse } from "@/types"

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
  refreshChangedCapabilities: vi.fn(),
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
import { useTorrentListController } from "@/hooks/useTorrentListController"
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
  groupCopies?: boolean
}

// Acts like a Layout: load the list.
function useLayout({ filters = FILTERS, columnFiltersExpr, search = "", sort = "added_on", groupCopies }: LayoutProps) {
  const list = useTorrentListController({
    instanceId: 1,
    filters,
    columnFiltersExpr,
    search,
    sort,
    order: "desc",
    groupCopies,
  })
  return { list }
}

function renderLayout(initialProps: LayoutProps = {}) {
  return renderHook((props: LayoutProps) => useLayout(props), { initialProps, wrapper: makeWrapper() })
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
})

afterEach(() => {
  cleanup()
  localStorage.clear()
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

  // regression: #3657
  it("loads the next server page while the last 100 rows in memory still wait to show", async () => {
    const { result } = renderLayout()
    await flush()
    act(() => result.current.list.reportNearEnd())
    act(() => result.current.list.reportNearEnd())
    await flush()
    expect(pagesRequested()).toContain(1)
    expect(result.current.list.torrents).toHaveLength(350)
    expect(result.current.list.loadedRows).toBe(300)
  })

  // regression: #3657
  it("shows the rows of a page that was still loading when the user reached the end", async () => {
    let sendPage: () => void = () => {}
    mockedApi.getTorrents.mockImplementation((_instanceId, params) => params.page === 0
      ? Promise.resolve(makeResponse({ torrents: rows(300), total: 350, hasMore: true }))
      : new Promise(resolve => { sendPage = () => resolve(makeResponse({ torrents: rows(50, 300), total: 350, hasMore: false })) }))
    const { result } = renderLayout()
    await flush()
    act(() => result.current.list.reportNearEnd())
    act(() => result.current.list.reportNearEnd())
    await flush()
    expect(result.current.list.isLoadingMore).toBe(true)

    act(() => result.current.list.reportNearEnd())
    sendPage()
    await flush()
    expect(result.current.list.loadedRows).toBe(350)
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

describe("useTorrentListController — Copy groups", () => {
  // The server sends a group as adjacent rows: the Group representative with
  // first_sorted, then the other members with the id only.
  const representative = (hash: string, id: string) =>
    makeTorrent({ hash, copy_group: { id, first_sorted: { instance_id: 1, hash } } })
  const member = (hash: string, id: string) => makeTorrent({ hash, copy_group: { id } })
  const visible = (result: { current: ReturnType<typeof useLayout> }) => result.current.list.visibleRows.map(row => row.hash)

  it("shows the loaded rows as they are on a flat list", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({ torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" })], total: 2 }))
    const { result } = renderLayout({ groupCopies: true })
    await flush()
    expect(result.current.list.visibleRows).toBe(result.current.list.torrents)
  })

  it("starts each group collapsed and opens one group at a time", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({
      torrents: [representative("A", "g1"), member("a1", "g1"), makeTorrent({ hash: "B" }), representative("C", "g2"), member("c1", "g2")],
      total: 5,
      copyGroupsReady: true,
    }))
    const { result } = renderLayout({ groupCopies: true })
    await flush()
    expect(result.current.list.torrents).toHaveLength(5)
    expect(visible(result)).toEqual(["A", "B", "C"])

    act(() => result.current.list.toggleGroup("g1"))
    expect(visible(result)).toEqual(["A", "a1", "B", "C"])

    act(() => result.current.list.toggleGroup("g2"))
    expect(visible(result)).toEqual(["A", "a1", "B", "C", "c1"])

    act(() => result.current.list.toggleGroup("g1"))
    expect(visible(result)).toEqual(["A", "B", "C", "c1"])
  })

  it("puts a group at the place of its first row and its representative on top when a merge moved a member", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({
      torrents: [member("a1", "g1"), makeTorrent({ hash: "B" }), representative("A", "g1")],
      total: 3,
      copyGroupsReady: true,
    }))
    const { result } = renderLayout({ groupCopies: true })
    await flush()
    expect(visible(result)).toEqual(["A", "B"])

    act(() => result.current.list.toggleGroup("g1"))
    expect(visible(result)).toEqual(["A", "a1", "B"])
  })

  it("closes the groups when the selection scope changes and keeps them open on a sort", async () => {
    mockedApi.getTorrents.mockResolvedValue(makeResponse({
      torrents: [representative("A", "g1"), member("a1", "g1")],
      total: 2,
      copyGroupsReady: true,
    }))
    const { result, rerender } = renderLayout({ groupCopies: true })
    await flush()
    act(() => result.current.list.toggleGroup("g1"))
    act(() => result.current.list.toggleGroup("g9"))

    rerender({ groupCopies: true, sort: "name" })
    await flush()
    expect([...result.current.list.groupExpansion]).toEqual(["g1", "g9"])

    const scopes: LayoutProps[] = [
      { filters: { ...FILTERS, status: ["downloading"] } },
      { columnFiltersExpr: "AddedOn > 1704067200" },
      { search: "linux" },
    ]
    for (const scope of scopes) {
      rerender({ groupCopies: true, sort: "name" })
      await flush()
      act(() => result.current.list.toggleGroup("g1"))

      rerender({ groupCopies: true, sort: "name", ...scope })
      await flush()
      expect(result.current.list.groupExpansion.size).toBe(0)
      expect(visible(result)).toEqual(["A"])
    }
  })

  it("keeps the load-more window in Filter Cross-Seeds when the option changes", async () => {
    const crossSeed = { ...FILTERS, expr: "Hash == \"h0\" || Hash == \"h1\"" }
    const crossRows = Array.from({ length: 300 }, (_, i) => ({ ...makeTorrent({ hash: `h${i}` }), instanceId: 1, instanceName: "qb" }))
    mockedApi.getCrossInstanceTorrents.mockResolvedValue(makeResponse({ isCrossInstance: true, crossInstanceTorrents: crossRows, total: 300 }))
    const { result, rerender } = renderLayout({ filters: crossSeed })
    await flush()
    act(() => result.current.list.reportNearEnd())
    expect(result.current.list.loadedRows).toBe(200)

    rerender({ filters: crossSeed, groupCopies: true })
    await flush()
    expect(result.current.list.loadedRows).toBe(200)
  })

  it("counts the visible rows in the load-more window", async () => {
    // 150 groups of two torrents: 300 rows in memory, 150 visible.
    const groups = Array.from({ length: 150 }, (_, i) => [representative(`r${i}`, `g${i}`), member(`m${i}`, `g${i}`)]).flat()
    mockedApi.getTorrents.mockImplementation(async (_instanceId, params) => params.page === 0
      ? makeResponse({ torrents: groups, total: 400, hasMore: true, copyGroupsReady: true })
      : makeResponse({ torrents: [], total: 400, hasMore: false, copyGroupsReady: true }))
    const { result } = renderLayout({ groupCopies: true })
    await flush()
    expect(result.current.list.loadedRows).toBe(100)

    act(() => result.current.list.reportNearEnd())
    expect(result.current.list.loadedRows).toBe(150)
    await flush()
    expect(mockedApi.getTorrents.mock.calls.map(([, params]) => params.page)).toContain(1)
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
