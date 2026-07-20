/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// ---- mocks -------------------------------------------------------------

vi.mock("@/lib/api", () => ({
  api: {
    listTorznabIndexers: vi.fn(),
    getCrossSeedSettings: vi.fn(),
    analyzeTorrentForCrossSeedSearch: vi.fn(),
    searchCrossSeedTorrent: vi.fn(),
    applyCrossSeedSearchResults: vi.fn(),
    getAsyncFilteringStatus: vi.fn(),
  },
}))

vi.mock("sonner", () => ({
  toast: {
    info: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    success: vi.fn(),
  },
}))

// Near-identity translator: returns the raw key so assertions can target stable
// strings. When the hook interpolates the original error text via {{message}}
// (rate-limit wrapping), we append it so the wrapped string still carries the
// "rate limit"/"429" substrings the hook re-inspects for toast duration and for
// preserving the error on close — matching real i18n output behavior.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { message?: string }) =>
      opts?.message ? `${key}:${opts.message}` : key,
  }),
}))

// The hook exposes its imperative search / refresh / close actions only through
// the props of the `crossSeedDialog` React element it returns. renderHook does
// not mount that element, so we read props directly off the element rather than
// rendering the real (heavy) dialog. Mock it to a no-op so the import resolves
// and is cheap.
type DialogProps = Record<string, unknown>
vi.mock("@/components/torrents/CrossSeedDialog", () => ({
  CrossSeedDialog: () => null,
}))

import { api } from "@/lib/api"
import { toast } from "sonner"
import { useCrossSeedSearch } from "@/hooks/useCrossSeedSearch"
import { makeTorrent } from "@/test/mockTorrent"
import type { CrossSeedTorrentSearchResponse, TorznabIndexer } from "@/types"

const mockedApi = vi.mocked(api, true)
const mockedToast = vi.mocked(toast, true)

// ---- fixtures ----------------------------------------------------------

function makeIndexer(overrides: Partial<TorznabIndexer> = {}): TorznabIndexer {
  return {
    id: 1,
    name: "indexer-1",
    base_url: "http://example",
    indexer_id: "idx-1",
    backend: "native",
    enabled: true,
    priority: 0,
    timeout_seconds: 30,
    capabilities: [],
    categories: [],
    last_test_status: "ok",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

function makeSearchResponse(
  overrides: Partial<CrossSeedTorrentSearchResponse> = {}
): CrossSeedTorrentSearchResponse {
  return {
    sourceTorrent: { name: "test-torrent", hash: "abcdef0123456789" },
    results: [],
    ...overrides,
  }
}

function makeResult(
  overrides: Partial<CrossSeedTorrentSearchResponse["results"][number]> = {}
): CrossSeedTorrentSearchResponse["results"][number] {
  return {
    indexer: "indexer-1",
    indexerId: 1,
    title: "Some Release",
    downloadUrl: "http://dl/1",
    size: 100,
    seeders: 5,
    leechers: 1,
    categoryId: 5000,
    categoryName: "TV",
    publishDate: "",
    downloadVolumeFactor: 1,
    uploadVolumeFactor: 1,
    guid: "guid-1",
    matchScore: 1,
    ...overrides,
  }
}

function makeWrapper(queryClient?: QueryClient) {
  const client =
    queryClient ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

type HookResult = ReturnType<typeof useCrossSeedSearch>

// NOTE ON COUPLING: Several tests reach into
// `result.current.crossSeedDialog.props` (via the `dialogProps` helper) to
// invoke imperative actions (onScopeSearch / onForceRefresh / onClose / onApply)
// and to read view state (canForceRefresh / error / selectedKeys). This couples
// the suite to the hook returning a React element whose props expose those
// actions — an implementation detail, since CrossSeedDialog is mocked to
// () => null and never actually rendered. This is a deliberate tradeoff for the
// hook's current API. If the hook is later refactored to expose these imperative
// actions on its plain return object instead of through a JSX element, prefer
// reading them from the return value directly and delete this helper.
// Read the latest dialog props off the element the hook returns.
function dialogProps(result: { current: HookResult }): DialogProps {
  const element = result.current.crossSeedDialog as { props: DialogProps }
  return element.props
}

// Render the hook and let the two background queries (indexers + settings)
// settle so canCrossSeedSearch / sortedEnabledIndexers reflect the data.
// Pass a shared QueryClient to spy on cache invalidation.
async function renderSettled(instanceId = 1, queryClient?: QueryClient) {
  const utils = renderHook(() => useCrossSeedSearch(instanceId), {
    wrapper: makeWrapper(queryClient),
  })
  await act(async () => {
    await Promise.resolve()
  })
  await waitFor(() => {
    expect(mockedApi.listTorznabIndexers).toHaveBeenCalled()
    expect(mockedApi.getCrossSeedSettings).toHaveBeenCalled()
  })
  // extra tick to flush query-cache state updates into the hook
  await act(async () => {
    await Promise.resolve()
  })
  return utils
}

// ---- tests -------------------------------------------------------------

describe("useCrossSeedSearch", () => {
  beforeEach(() => {
    mockedApi.listTorznabIndexers.mockResolvedValue([makeIndexer()])
    mockedApi.getCrossSeedSettings.mockResolvedValue({
      findIndividualEpisodes: false,
    } as never)
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValue({
      name: "test-torrent",
      hash: "abcdef0123456789",
      contentFilteringCompleted: true,
    } as never)
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(makeSearchResponse())
    mockedApi.getAsyncFilteringStatus.mockResolvedValue({
      capabilitiesCompleted: true,
      contentCompleted: true,
      capabilityIndexers: [],
      filteredIndexers: [],
      excludedIndexers: {},
      contentMatches: [],
    } as never)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it("gates canCrossSeedSearch on having at least one enabled indexer", async () => {
    mockedApi.listTorznabIndexers.mockResolvedValue([
      makeIndexer({ id: 1, enabled: false }),
    ])
    const { result } = await renderSettled()
    expect(result.current.canCrossSeedSearch).toBe(false)
  })

  it("enables canCrossSeedSearch once an indexer is enabled", async () => {
    mockedApi.listTorznabIndexers.mockResolvedValue([
      makeIndexer({ id: 1, enabled: false }),
      makeIndexer({ id: 2, enabled: true }),
    ])
    const { result } = await renderSettled()
    expect(result.current.canCrossSeedSearch).toBe(true)
  })

  it("blocks incomplete torrents with a completed-only toast and does not analyze", async () => {
    const { result } = await renderSettled()

    act(() => {
      result.current.openCrossSeedSearch(makeTorrent({ progress: 0.5 }))
    })

    expect(mockedToast.info).toHaveBeenCalledWith("hooks.search.completedOnly")
    expect(mockedApi.analyzeTorrentForCrossSeedSearch).not.toHaveBeenCalled()
  })

  it("blocks when there are no enabled indexers", async () => {
    mockedApi.listTorznabIndexers.mockResolvedValue([
      makeIndexer({ enabled: false }),
    ])
    const { result } = await renderSettled()

    act(() => {
      result.current.openCrossSeedSearch(makeTorrent({ progress: 1 }))
    })

    expect(mockedToast.error).toHaveBeenCalledWith("hooks.search.noIndexers")
    expect(mockedApi.analyzeTorrentForCrossSeedSearch).not.toHaveBeenCalled()
  })

  it("analyzes the source torrent on the happy-path open", async () => {
    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-open" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    expect(mockedApi.analyzeTorrentForCrossSeedSearch).toHaveBeenCalledTimes(1)
    expect(mockedApi.analyzeTorrentForCrossSeedSearch).toHaveBeenCalledWith(
      1,
      "hash-open"
    )
  })

  it("runs a search, toggles searching state, and selects all results by default", async () => {
    let resolveSearch!: (r: CrossSeedTorrentSearchResponse) => void
    mockedApi.searchCrossSeedTorrent.mockReturnValue(
      new Promise<CrossSeedTorrentSearchResponse>(resolve => {
        resolveSearch = resolve
      })
    )

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-search" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    // Trigger the scoped search through the dialog props.
    act(() => {
      ;(dialogProps(result).onScopeSearch as () => void)()
    })

    expect(result.current.isCrossSeedSearching).toBe(true)

    const results = [
      makeResult({ guid: "guid-a", downloadUrl: "http://dl/a" }),
      makeResult({ guid: "guid-b", downloadUrl: "http://dl/b" }),
    ]
    await act(async () => {
      resolveSearch(makeSearchResponse({ results }))
      await Promise.resolve()
    })

    expect(result.current.isCrossSeedSearching).toBe(false)
    // all result keys selected by default (guid is the key)
    const selected = dialogProps(result).selectedKeys as Set<string>
    expect(selected.has("guid-a")).toBe(true)
    expect(selected.has("guid-b")).toBe(true)
    expect(selected.size).toBe(2)
  })

  it("forwards the partial flag from the search response to the dialog", async () => {
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results: [makeResult()], partial: true })
    )

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-partial-flag" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    expect(dialogProps(result).partial).toBe(true)
  })

  it("forwards sorted indexer ids, excludes filtered ids, and forwards findIndividualEpisodes", async () => {
    mockedApi.getCrossSeedSettings.mockResolvedValue({
      findIndividualEpisodes: true,
    } as never)
    mockedApi.listTorznabIndexers.mockResolvedValue([
      makeIndexer({ id: 1, name: "bravo", enabled: true, priority: 1 }),
      makeIndexer({ id: 2, name: "alpha", enabled: true, priority: 5 }),
      makeIndexer({ id: 3, name: "charlie", enabled: true, priority: 1 }),
    ])
    // analyze returns an excluded indexer (id 3) -> must be filtered out
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValue({
      name: "t",
      hash: "hash-scope",
      contentFilteringCompleted: true,
      excludedIndexers: { 3: "no caps" },
    } as never)

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-scope" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    expect(mockedApi.searchCrossSeedTorrent).toHaveBeenCalledTimes(1)
    const [calledInstanceId, calledHash, options] =
      mockedApi.searchCrossSeedTorrent.mock.calls[0]
    expect(calledInstanceId).toBe(1)
    expect(calledHash).toBe("hash-scope")
    // priority desc (id 2 first), then name asc for ties (bravo before charlie),
    // with excluded id 3 removed.
    expect(options?.indexerIds).toEqual([2, 1])
    expect(options?.findIndividualEpisodes).toBe(true)
  })

  it("shows a no-matches toast when the search returns no results", async () => {
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results: [] })
    )
    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-empty" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    expect(mockedToast.info).toHaveBeenCalledWith("hooks.search.noMatches")
  })

  it("applies a cooldown on force-refresh and recovers after 30s", async () => {
    const { result } = await renderSettled()
    // Switch to fake timers only after the background queries have settled so
    // renderSettled's real-timer waitFor isn't starved.
    vi.useFakeTimers()
    const torrent = makeTorrent({ progress: 1, hash: "hash-refresh" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    // force refresh available before use
    expect(dialogProps(result).canForceRefresh).toBe(true)

    await act(async () => {
      ;(dialogProps(result).onForceRefresh as () => void)()
      await Promise.resolve()
    })

    // bypass cache requested
    const refreshCall = mockedApi.searchCrossSeedTorrent.mock.calls.at(-1)
    expect(refreshCall?.[2]?.cacheMode).toBe("bypass")

    // cooldown engaged -> not available and a label is exposed
    expect(dialogProps(result).canForceRefresh).toBe(false)
    expect(dialogProps(result).refreshCooldownLabel).toBe(
      "hooks.search.refreshReadyIn"
    )

    // after the cooldown elapses it becomes available again
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(dialogProps(result).canForceRefresh).toBe(true)
    expect(dialogProps(result).refreshCooldownLabel).toBeUndefined()
  })

  it("polls async filtering status and re-analyzes once content completes", async () => {
    // open with an incomplete-filtering source torrent so polling starts
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValueOnce({
      name: "t",
      hash: "hash-poll",
      contentFilteringCompleted: false,
    } as never)
    // first poll: not done; second poll: done
    mockedApi.getAsyncFilteringStatus
      .mockResolvedValueOnce({
        capabilitiesCompleted: true,
        contentCompleted: false,
        capabilityIndexers: [],
        filteredIndexers: [],
        excludedIndexers: {},
        contentMatches: [],
      } as never)
      .mockResolvedValue({
        capabilitiesCompleted: true,
        contentCompleted: true,
        capabilityIndexers: [],
        filteredIndexers: [],
        excludedIndexers: {},
        contentMatches: [],
      } as never)
    // re-analyze after completion
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValue({
      name: "t",
      hash: "hash-poll",
      contentFilteringCompleted: true,
    } as never)

    const { result } = await renderSettled()
    vi.useFakeTimers()
    const torrent = makeTorrent({ progress: 1, hash: "hash-poll" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    // analyze called once for the open
    expect(mockedApi.analyzeTorrentForCrossSeedSearch).toHaveBeenCalledTimes(1)

    // first interval tick -> contentCompleted false
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(mockedApi.getAsyncFilteringStatus).toHaveBeenCalledTimes(1)

    // second tick -> contentCompleted true -> re-analyze + clear interval
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(mockedApi.getAsyncFilteringStatus).toHaveBeenCalledTimes(2)
    expect(mockedApi.analyzeTorrentForCrossSeedSearch).toHaveBeenCalledTimes(2)

    const callsAfterComplete = mockedApi.getAsyncFilteringStatus.mock.calls.length
    // interval cleared: no further polls
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })
    expect(mockedApi.getAsyncFilteringStatus.mock.calls.length).toBe(
      callsAfterComplete
    )
  })

  it("stops polling once the dialog is closed", async () => {
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValue({
      name: "t",
      hash: "hash-close",
      contentFilteringCompleted: false,
    } as never)
    mockedApi.getAsyncFilteringStatus.mockResolvedValue({
      capabilitiesCompleted: true,
      contentCompleted: false,
      capabilityIndexers: [],
      filteredIndexers: [],
      excludedIndexers: {},
      contentMatches: [],
    } as never)

    const { result } = await renderSettled()
    vi.useFakeTimers()
    const torrent = makeTorrent({ progress: 1, hash: "hash-close" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(mockedApi.getAsyncFilteringStatus).toHaveBeenCalledTimes(1)

    // close the dialog
    await act(async () => {
      ;(dialogProps(result).onClose as () => void)()
      await Promise.resolve()
    })

    const callsAtClose = mockedApi.getAsyncFilteringStatus.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })
    expect(mockedApi.getAsyncFilteringStatus.mock.calls.length).toBe(callsAtClose)
  })

  it("wraps rate-limit search failures and preserves the error on close", async () => {
    mockedApi.searchCrossSeedTorrent.mockRejectedValue(
      new Error("429 too many requests rate limit")
    )
    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-429" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    // The raw error text is classified as rate-limited, wrapped via the
    // rateLimitSearch key, and surfaced with the long (10s) toast duration.
    expect(mockedToast.error).toHaveBeenCalledWith(
      expect.stringContaining("hooks.search.rateLimitSearch"),
      { duration: 10000 }
    )
    expect(result.current.isCrossSeedSearching).toBe(false)
    // error surfaced on the dialog
    const errorBeforeClose = dialogProps(result).error
    expect(errorBeforeClose).toBeTruthy()

    // closing preserves the rate-limit error (resetCrossSeedState(true))
    await act(async () => {
      ;(dialogProps(result).onClose as () => void)()
      await Promise.resolve()
    })
    expect(dialogProps(result).error).toBe(errorBeforeClose)
  })

  it("warns and clears the interval when polling status rejects", async () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {})
    mockedApi.analyzeTorrentForCrossSeedSearch.mockResolvedValue({
      name: "t",
      hash: "hash-poll-err",
      contentFilteringCompleted: false,
    } as never)
    mockedApi.getAsyncFilteringStatus.mockRejectedValue(
      new Error("status boom")
    )

    const { result } = await renderSettled()
    vi.useFakeTimers()
    const torrent = makeTorrent({ progress: 1, hash: "hash-poll-err" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(mockedApi.getAsyncFilteringStatus).toHaveBeenCalledTimes(1)
    expect(warnSpy).toHaveBeenCalled()

    // interval cleared: no infinite retry
    const callsAfterError = mockedApi.getAsyncFilteringStatus.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })
    expect(mockedApi.getAsyncFilteringStatus.mock.calls.length).toBe(
      callsAfterError
    )
    warnSpy.mockRestore()
  })

  it("applies selected results and invalidates torrent queries", async () => {
    const results = [makeResult({ guid: "guid-apply" })]
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results })
    )
    mockedApi.applyCrossSeedSearchResults.mockResolvedValue({
      results: [
        {
          title: "Some Release",
          indexer: "indexer-1",
          success: true,
          instanceResults: [
            {
              instanceId: 1,
              instanceName: "inst",
              success: true,
              status: "added",
            },
          ],
        },
      ],
    } as never)

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries")

    const { result } = await renderSettled(1, queryClient)
    const torrent = makeTorrent({ progress: 1, hash: "hash-apply" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    await act(async () => {
      await (dialogProps(result).onApply as () => Promise<void>)()
    })

    expect(mockedApi.applyCrossSeedSearchResults).toHaveBeenCalledTimes(1)
    const [applyInstanceId, applyHash, payload] =
      mockedApi.applyCrossSeedSearchResults.mock.calls[0]
    expect(applyInstanceId).toBe(1)
    expect(applyHash).toBe("hash-apply")
    expect(payload.selections).toHaveLength(1)
    expect(payload.selections[0].guid).toBe("guid-apply")

    // status:"added" with no failures -> the addedCount success branch.
    expect(mockedToast.success).toHaveBeenCalledWith("hooks.search.applySuccess")
    expect(mockedToast.warning).not.toHaveBeenCalled()
    expect(mockedToast.error).not.toHaveBeenCalled()

    // both torrent caches invalidated so the list/counts refresh after apply
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["torrents-list", 1],
      exact: false,
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["torrent-counts", 1],
      exact: false,
    })
  })

  it("maps partial apply results (some added, some failed) to a warning", async () => {
    const results = [makeResult({ guid: "guid-partial" })]
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results })
    )
    mockedApi.applyCrossSeedSearchResults.mockResolvedValue({
      results: [
        {
          title: "Some Release",
          indexer: "indexer-1",
          success: true,
          instanceResults: [
            {
              instanceId: 1,
              instanceName: "a",
              success: true,
              status: "added",
            },
            {
              instanceId: 2,
              instanceName: "b",
              success: false,
              status: "error",
            },
          ],
        },
      ],
    } as never)

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-partial" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })
    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })
    await act(async () => {
      await (dialogProps(result).onApply as () => Promise<void>)()
    })

    expect(mockedToast.warning).toHaveBeenCalledWith("hooks.search.applyPartial")
    expect(mockedToast.success).not.toHaveBeenCalled()
  })

  it("maps an all-failed apply result to an error toast", async () => {
    const results = [makeResult({ guid: "guid-failed" })]
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results })
    )
    mockedApi.applyCrossSeedSearchResults.mockResolvedValue({
      results: [
        {
          title: "Some Release",
          indexer: "indexer-1",
          success: false,
          instanceResults: [
            {
              instanceId: 1,
              instanceName: "a",
              success: false,
              status: "error",
            },
          ],
        },
      ],
    } as never)

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-failed" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })
    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })
    await act(async () => {
      await (dialogProps(result).onApply as () => Promise<void>)()
    })

    expect(mockedToast.error).toHaveBeenCalledWith("hooks.search.applyFailed")
    expect(mockedToast.success).not.toHaveBeenCalled()
  })

  it("warns without calling the API when applying with no selection", async () => {
    const results = [makeResult({ guid: "guid-none" })]
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results })
    )

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-none" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })
    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    // deselect the only result so the selection set is empty
    act(() => {
      ;(
        dialogProps(result).onClearSelection as () => void
      )()
    })

    await act(async () => {
      await (dialogProps(result).onApply as () => Promise<void>)()
    })

    expect(mockedToast.warning).toHaveBeenCalledWith("hooks.search.selectResult")
    expect(mockedApi.applyCrossSeedSearchResults).not.toHaveBeenCalled()
  })

  it("surfaces an apply failure via toast.error", async () => {
    const results = [makeResult({ guid: "guid-throw" })]
    mockedApi.searchCrossSeedTorrent.mockResolvedValue(
      makeSearchResponse({ results })
    )
    mockedApi.applyCrossSeedSearchResults.mockRejectedValue(
      new Error("apply boom")
    )

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-throw" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })
    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })
    await act(async () => {
      await (dialogProps(result).onApply as () => Promise<void>)()
    })

    expect(mockedToast.error).toHaveBeenCalledWith("apply boom")
    expect(mockedToast.success).not.toHaveBeenCalled()
  })

  it("wraps rate-limit analyze failures on open and surfaces them", async () => {
    mockedApi.analyzeTorrentForCrossSeedSearch.mockRejectedValue(
      new Error("429 rate limit")
    )

    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-analyze-429" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    // analyze is the first network call on open; its rate-limited rejection is
    // wrapped via the DISTINCT rateLimitAnalyze key with the long toast duration.
    expect(mockedToast.error).toHaveBeenCalledWith(
      expect.stringContaining("hooks.search.rateLimitAnalyze"),
      { duration: 10000 }
    )
    expect(dialogProps(result).error).toBeTruthy()
    expect(dialogProps(result).error).toEqual(
      expect.stringContaining("hooks.search.rateLimitAnalyze")
    )
  })

  it("warns when scope-searching in custom mode with no indexer selected", async () => {
    const { result } = await renderSettled()
    const torrent = makeTorrent({ progress: 1, hash: "hash-custom-empty" })

    await act(async () => {
      result.current.openCrossSeedSearch(torrent)
      await Promise.resolve()
    })

    // switch to custom indexer mode without selecting any indexer
    act(() => {
      ;(
        dialogProps(result).onIndexerModeChange as (m: "all" | "custom") => void
      )("custom")
    })

    await act(async () => {
      ;(dialogProps(result).onScopeSearch as () => void)()
      await Promise.resolve()
    })

    expect(mockedToast.warning).toHaveBeenCalledWith("hooks.search.selectTracker")
    expect(mockedApi.searchCrossSeedTorrent).not.toHaveBeenCalled()
  })
})
