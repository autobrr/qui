/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    bulkAction: vi.fn(),
    renameTorrent: vi.fn(),
    renameTorrentFile: vi.fn(),
    renameTorrentFolder: vi.fn(),
  },
}))

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

// Preserve the interpolated count so success toasts can be asserted against it
// (mirrors useTorrentExporter.test.tsx). Without this, react-i18next stays
// uninitialized and the passthrough t() would silently drop {count}.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) =>
      opts && "count" in opts ? `${key}/${opts.count}` : key,
  }),
}))

import { api } from "@/lib/api"
import { toast } from "sonner"
import { resetPendingListRefetchesForTests, scheduleTorrentListRefetches, useTorrentActions } from "@/hooks/useTorrentActions"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { ActionTarget, SelectAllTarget } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
import type { Torrent, TorrentFilters } from "@/types"

const mockedApi = vi.mocked(api, true)
const mockedToast = vi.mocked(toast, true)

const INSTANCE_ID = 7

/** Build a fresh QueryClient + wrapper, returning both so tests can inspect the cache. */
function makeHarness() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const refetchSpy = vi.spyOn(queryClient, "refetchQueries").mockResolvedValue(undefined)
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue(undefined)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper, refetchSpy, invalidateSpy }
}

function renderActions(opts?: {
  instanceIds?: number[]
  onActionComplete?: (action: string) => void
}) {
  const harness = makeHarness()
  const onActionComplete = opts?.onActionComplete ?? vi.fn()
  const { result } = renderHook(
    () =>
      useTorrentActions({
        instanceId: INSTANCE_ID,
        instanceIds: opts?.instanceIds,
        onActionComplete: onActionComplete as never,
      }),
    { wrapper: harness.wrapper }
  )
  return { ...harness, result, onActionComplete }
}

// Ticks on one instance: plain hashes.
function ticks(...hashes: string[]): ActionTarget {
  return { kind: "ticks", hashes, count: hashes.length, previewTorrents: hashes.map(hash => makeTorrent({ hash })) }
}

// Ticks on the cross-instance endpoint: instance-and-hash pairs.
function pairs(targets: TorrentActionTarget[], previewTorrents: Torrent[] = []): ActionTarget {
  return { kind: "ticks", hashes: [], targets, count: targets.length, previewTorrents }
}

function selectAll(count: number, part: Partial<SelectAllTarget> = {}, loaded: string[] = []): ActionTarget {
  return { kind: "selectAll", selectAll: { filters: makeFilters(), search: "", ...part }, count, previewTorrents: loaded.map(hash => makeTorrent({ hash })) }
}

const TORRENTS_LIST_KEY = (extra: unknown = "page-1") => ["torrents-list", INSTANCE_ID, extra]

beforeEach(() => {
  vi.useFakeTimers()
  resetPendingListRefetchesForTests()
  localStorage.clear()
  mockedApi.bulkAction.mockResolvedValue(undefined as never)
  mockedApi.renameTorrent.mockResolvedValue(undefined as never)
  mockedApi.renameTorrentFile.mockResolvedValue(undefined as never)
  mockedApi.renameTorrentFolder.mockResolvedValue(undefined as never)
})

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe("useTorrentActions - delete", () => {
  it("optimistically removes deleted hashes from the cache and decrements totals (non-select-all)", async () => {
    const { result, queryClient } = renderActions()

    queryClient.setQueryData(TORRENTS_LIST_KEY(), {
      torrents: [
        makeTorrent({ hash: "h1" }),
        makeTorrent({ hash: "h2" }),
        makeTorrent({ hash: "h3" }),
      ],
      total: 3,
      totalCount: 10,
    })

    await act(async () => {
      await result.current.handleDelete(ticks("h1", "h2"))
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    const payload = mockedApi.bulkAction.mock.calls[0][1]
    expect(payload.action).toBe("delete")
    expect(payload.hashes).toEqual(["h1", "h2"])
    expect(payload.selectAll).toBeFalsy()

    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY()) as {
      torrents: { hash: string }[]
      total: number
      totalCount: number
    }
    expect(cache.torrents.map(t => t.hash)).toEqual(["h3"])
    // clientCount defaults to clientHashes.length === 2
    expect(cache.total).toBe(1)
    expect(cache.totalCount).toBe(8)
    // The success toast carries the same count (2) used for optimism.
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/2")
  })

  it("select-all delete sends empty hashes + selectAll, passes filters through, and uses clientHashes for optimism", async () => {
    const { result, queryClient } = renderActions()

    queryClient.setQueryData(TORRENTS_LIST_KEY(), {
      torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" }), makeTorrent({ hash: "c" })],
      total: 100,
      totalCount: 100,
    })

    const filters: TorrentFilters = {
      status: [],
      categories: ["raw"],
      tags: [],
      trackers: [],
      expandedCategories: ["movies", "tv"],
      expandedExcludeCategories: ["junk"],
    } as unknown as TorrentFilters

    await act(async () => {
      await result.current.handleDelete(selectAll(100, { filters, search: "ubuntu", excludeHashes: ["zz"] }, ["a", "b"]))
    })

    const payload = mockedApi.bulkAction.mock.calls[0][1]
    expect(payload.hashes).toEqual([])
    expect(payload.selectAll).toBe(true)
    expect(payload.search).toBe("ubuntu")
    expect(payload.excludeHashes).toEqual(["zz"])
    // Filters go to api.bulkAction untouched; the expandedCategories fold is
    // the api client's job (ADR 0010, pinned in api.torrents.test.ts).
    expect(payload.filters).toBe(filters)

    // Optimism uses clientHashes (a,b) not the empty hashes array; clientCount drives totals
    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY()) as {
      torrents: { hash: string }[]
      total: number
      totalCount: number
    }
    expect(cache.torrents.map(t => t.hash)).toEqual(["c"])
    expect(cache.total).toBe(0)
    expect(cache.totalCount).toBe(0)
    // clientCount (totalSelected=100) drives the toast, overriding both the
    // empty outgoing hashes array and clientHashes.length (2).
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/100")
  })

  it("delete success closes dialog, resets cross-seed flag, fires onActionComplete and the plain delete toast", async () => {
    const { result, onActionComplete } = renderActions()

    act(() => {
      result.current.prepareDeleteAction()
    })
    expect(result.current.showDeleteDialog).toBe(true)

    await act(async () => {
      result.current.setDeleteCrossSeeds(true)
    })

    await act(async () => {
      await result.current.handleDelete(ticks("h1"))
    })

    expect(result.current.showDeleteDialog).toBe(false)
    expect(result.current.deleteCrossSeeds).toBe(false)
    expect(onActionComplete).toHaveBeenCalledWith("delete")
    // Single-hash delete resolves the plain key with count=1.
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/1")
  })

  it("uses the deleteWithFiles toast key and the 5000ms refetch branch when deleteFiles is locked-on", async () => {
    // Seed persisted deleteFiles=true so usePersistedDeleteFiles starts true
    localStorage.setItem("qui-delete-files-lock", JSON.stringify(true))
    localStorage.setItem("qui-delete-files-default", JSON.stringify(true))

    const { result, refetchSpy } = renderActions()

    await act(async () => {
      await result.current.handleDelete(ticks("h1"))
    })

    const payload = mockedApi.bulkAction.mock.calls[0][1]
    expect(payload.deleteFiles).toBe(true)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.deleteWithFiles/1")

    // No refetch before the 5s delay elapses...
    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(refetchSpy).not.toHaveBeenCalled()

    // ...but it fires once 5000ms total has passed.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    const refetchedKeys = refetchSpy.mock.calls.map(c => (c[0] as { queryKey: unknown[] }).queryKey[0])
    expect(refetchedKeys).toContain("torrents-list")
    expect(refetchedKeys).not.toContain("torrent-counts")
  })

  it("uses the 2000ms refetch branch when deleteFiles is false", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      await result.current.handleDelete(ticks("h1"))
    })

    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1999)
    })
    expect(refetchSpy).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(refetchSpy.mock.calls.length).toBeGreaterThan(0)
  })

  it("ERROR PATH: a rejected delete fires an error toast and leaves the seeded cache untouched (optimism is success-only)", async () => {
    const { result, queryClient } = renderActions()

    queryClient.setQueryData(TORRENTS_LIST_KEY(), {
      torrents: [makeTorrent({ hash: "h1" }), makeTorrent({ hash: "h2" })],
      total: 2,
      totalCount: 2,
    })

    mockedApi.bulkAction.mockRejectedValueOnce(new Error("boom"))

    await act(async () => {
      await result.current.handleDelete(ticks("h1", "h2")).catch(() => undefined)
    })

    expect(mockedToast.error).toHaveBeenCalledWith(
      "actionToasts.failed.delete/2",
      { description: "boom" }
    )
    // Cache must be unchanged — there is no optimistic mutation on failure.
    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY()) as {
      torrents: { hash: string }[]
      total: number
      totalCount: number
    }
    expect(cache.torrents.map(t => t.hash)).toEqual(["h1", "h2"])
    expect(cache.total).toBe(2)
    expect(cache.totalCount).toBe(2)
  })

  it("select-all delete sends the chosen cross-seeds in a second targeted request", async () => {
    const { result } = renderActions()
    const crossSeedTargets = [
      { instanceId: 7, hash: "cs1" },
      { instanceId: 9, hash: "cs2" },
    ]

    await act(async () => {
      await result.current.handleDelete(selectAll(50, {}, ["a"]), crossSeedTargets)
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(2)
    const first = mockedApi.bulkAction.mock.calls[0][1]
    expect(first.selectAll).toBe(true)
    expect(first.hashes).toEqual([])
    const second = mockedApi.bulkAction.mock.calls[1][1]
    expect(second.selectAll).toBeFalsy()
    expect(second.hashes).toEqual([])
    expect(second.targets).toEqual(crossSeedTargets)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/50")
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/2")
  })

  it("deleteFiles applies to the cross-seed request too", async () => {
    // Seed persisted deleteFiles=true so usePersistedDeleteFiles starts true
    localStorage.setItem("qui-delete-files-lock", JSON.stringify(true))
    localStorage.setItem("qui-delete-files-default", JSON.stringify(true))

    const { result } = renderActions()
    const crossSeedTargets = [
      { instanceId: 7, hash: "cs1" },
      { instanceId: 9, hash: "cs2" },
    ]

    await act(async () => {
      await result.current.handleDelete(selectAll(50, {}, ["a"]), crossSeedTargets)
    })

    expect(mockedApi.bulkAction.mock.calls[0][1].deleteFiles).toBe(true)
    expect(mockedApi.bulkAction.mock.calls[1][1].deleteFiles).toBe(true)
  })

  it("select-all delete also sends a cross-seed that the user unticked from the select-all", async () => {
    // The dialog lists an unticked cross-seed like any other cross-seed that is not selected,
    // so "Also delete these cross-seeded torrents" deletes it too.
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleDelete(selectAll(50, { excludeHashes: ["cs1"] }, ["a"]), [{ instanceId: 7, hash: "cs1" }])
    })

    expect(mockedApi.bulkAction.mock.calls[0][1].excludeHashes).toEqual(["cs1"])
    expect(mockedApi.bulkAction.mock.calls[1][1].targets).toEqual([{ instanceId: 7, hash: "cs1" }])
  })

  it("select-all delete rejects when the cross-seed request fails, after request 1 succeeded", async () => {
    const { result } = renderActions()
    mockedApi.bulkAction.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error("boom"))

    await act(async () => {
      await expect(result.current.handleDelete(selectAll(50, {}, ["a"]), [{ instanceId: 7, hash: "cs1" }])).rejects.toThrow("boom")
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(2)
    expect(mockedToast.error).toHaveBeenCalledTimes(1)
  })

  it("non-select-all delete lists the cross-seeds in the same request, as plain hashes on one instance", async () => {
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleDelete(ticks("h1"), [{ instanceId: 7, hash: "cs1" }])
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    const request = mockedApi.bulkAction.mock.calls[0][1]
    expect(request.hashes).toEqual(["h1", "cs1"])
    expect(request.targets).toBeUndefined()
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/2")
  })

  it("non-select-all delete lists the cross-seeds as pairs, and no bare hash, on the cross-instance endpoint", async () => {
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleDelete(pairs([{ instanceId: 7, hash: "h1" }]), [{ instanceId: 7, hash: "cs1" }])
    })

    const request = mockedApi.bulkAction.mock.calls[0][1]
    expect(request.hashes).toEqual([])
    expect(request.targets).toEqual([{ instanceId: 7, hash: "h1" }, { instanceId: 7, hash: "cs1" }])
  })

  // A request on one instance leaves out targets on other instances.
  // regression: #3592
  it("sends the cross-seeds to the unified scope when one is on another instance", async () => {
    const { result } = renderActions()
    const crossSeedTargets = [{ instanceId: 7, hash: "cs1" }, { instanceId: 9, hash: "cs2" }]

    await act(async () => {
      await result.current.handleDelete(pairs([{ instanceId: 7, hash: "h1" }]), crossSeedTargets)
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(2)
    const [firstScope, first] = mockedApi.bulkAction.mock.calls[0]
    expect(firstScope).toBe(7)
    expect(first.targets).toEqual([{ instanceId: 7, hash: "h1" }])
    const [secondScope, second] = mockedApi.bulkAction.mock.calls[1]
    expect(secondScope).toBe(0)
    expect(second.targets).toEqual(crossSeedTargets)
    expect(second.hashes).toEqual([])
  })

  it("leaves a cross-seed on another instance out of the cached total of this instance", async () => {
    const { result, queryClient } = renderActions()
    queryClient.setQueryData(TORRENTS_LIST_KEY(), {
      torrents: [makeTorrent({ hash: "h1" }), makeTorrent({ hash: "cs1" }), makeTorrent({ hash: "keep" })],
      total: 10,
      totalCount: 10,
    })

    await act(async () => {
      await result.current.handleDelete(ticks("h1"), [{ instanceId: 7, hash: "cs1" }, { instanceId: 9, hash: "cs2" }])
    })

    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY()) as { total: number; totalCount: number }
    expect(cache.total).toBe(8)
    expect(cache.totalCount).toBe(8)
  })
})

describe("useTorrentActions - tags", () => {
  it("happy path issues sequential removeTags then addTags with comma-joined tags, closes dialog, toasts, and completes", async () => {
    const { result, onActionComplete } = renderActions()

    await act(async () => {
      await result.current.handleUpdateTags({ add: ["new1", "new2"], remove: ["old1"] }, ticks("h1", "h2"))
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(2)
    // remove runs first
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({
      action: "removeTags",
      tags: "old1",
    })
    expect(mockedApi.bulkAction.mock.calls[1][1]).toMatchObject({
      action: "addTags",
      tags: "new1,new2",
    })

    expect(result.current.showTagsDialog).toBe(false)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.updatedTags/2")
    expect(onActionComplete).toHaveBeenCalledWith("setTags")
  })

  it("empty plan short-circuits: no bulkAction call, dialog closed", async () => {
    const { result } = renderActions()

    act(() => {
      result.current.prepareTagsAction()
    })
    expect(result.current.showTagsDialog).toBe(true)

    await act(async () => {
      await result.current.handleUpdateTags({ add: [], remove: [] }, ticks("h1"))
    })

    expect(mockedApi.bulkAction).not.toHaveBeenCalled()
    expect(result.current.showTagsDialog).toBe(false)
  })

  it("ERROR PATH (partial failure): removeTags resolves, addTags rejects -> partialTags toast, dialog closed", async () => {
    const { result } = renderActions()

    // Open the dialog so we can prove the onError
    // TagBulkActionError branch (not the mutationFn) closes it.
    act(() => {
      result.current.prepareTagsAction()
    })
    expect(result.current.showTagsDialog).toBe(true)

    // First call (removeTags) succeeds, second call (addTags) rejects.
    mockedApi.bulkAction
      .mockResolvedValueOnce(undefined as never)
      .mockRejectedValueOnce(new Error("add failed"))

    await act(async () => {
      await result.current
        .handleUpdateTags({ add: ["new1"], remove: ["old1"] }, ticks("h1"))
        .catch(() => undefined)
    })

    // partialTags.title (count=1), not updateTagsFailed
    const errorKeys = mockedToast.error.mock.calls.map(c => c[0])
    expect(errorKeys).toContain("actionToasts.partialTags.title/1")
    expect(errorKeys.some(k => String(k).startsWith("actionToasts.updateTagsFailed"))).toBe(false)

    // onError TagBulkActionError branch closes the dialog.
    expect(result.current.showTagsDialog).toBe(false)
  })
})

describe("useTorrentActions - non-delete success transitions", () => {
  it("resume uses the 2000ms refetch branch", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      result.current.handleAction("resume", ticks("h1"))
    })
    // flush the resolved mutation's onSuccess
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })

    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1999)
    })
    expect(refetchSpy).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    const refetchedKeys = refetchSpy.mock.calls.map(c => (c[0] as { queryKey: unknown[] }).queryKey[0])
    expect(refetchedKeys).toContain("torrents-list")
    expect(refetchedKeys).not.toContain("torrent-counts")
  })

  it("setLocation uses the default 1000ms refetch branch and closes the location dialog", async () => {
    const { result, refetchSpy, onActionComplete } = renderActions()

    act(() => {
      result.current.setShowLocationDialog(true)
    })
    expect(result.current.showLocationDialog).toBe(true)

    await act(async () => {
      await result.current.handleSetLocation("/data/new", ticks("h1"))
    })

    // Dialog closes on success.
    expect(result.current.showLocationDialog).toBe(false)
    expect(onActionComplete).toHaveBeenCalledWith("setLocation")
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setLocation/1")

    // Default (non-resume/forceStart) branch refetches at 1000ms, not 2000ms.
    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(999)
    })
    expect(refetchSpy).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    const refetchedKeys = refetchSpy.mock.calls.map(c => (c[0] as { queryKey: unknown[] }).queryKey[0])
    expect(refetchedKeys).toContain("torrents-list")
    expect(refetchedKeys).not.toContain("torrent-counts")
  })
})

describe("useTorrentActions - rename", () => {
  it("handleRenameFile with an unchanged trimmed path short-circuits without calling the API", async () => {
    const { result } = renderActions()

    act(() => {
      result.current.setShowRenameFileDialog(true)
    })

    await act(async () => {
      await result.current.handleRenameFile("h1", "  same/path.mkv  ", "same/path.mkv")
    })

    expect(mockedApi.renameTorrentFile).not.toHaveBeenCalled()
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.renameFileUnchanged")
    expect(result.current.showRenameFileDialog).toBe(false)
  })

  it("handleRenameFile with a changed path calls the API and invalidates torrent-files", async () => {
    const { result, invalidateSpy } = renderActions()

    await act(async () => {
      await result.current.handleRenameFile("h1", "old/path.mkv", "new/path.mkv")
    })

    expect(mockedApi.renameTorrentFile).toHaveBeenCalledWith(INSTANCE_ID, "h1", "old/path.mkv", "new/path.mkv")
    const invalidatedKeys = invalidateSpy.mock.calls.map(
      c => (c[0] as { queryKey: unknown[] }).queryKey
    )
    expect(invalidatedKeys).toContainEqual(["torrent-files", INSTANCE_ID, "h1"])
  })

  it("handleRenameTorrent rejects empty names with an error toast and no API call", async () => {
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleRenameTorrent("h1", "   ")
    })

    expect(mockedApi.renameTorrent).not.toHaveBeenCalled()
    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.renameTorrentEmpty")
  })
})

describe("useTorrentActions - prepare helpers", () => {
  it("prepareRecheckAction opens the dialog for many torrents but acts immediately for a single one", async () => {
    const { result } = renderActions()

    // count > 1 -> open dialog, no immediate bulkAction
    act(() => {
      result.current.prepareRecheckAction(ticks("h1", "h2"))
    })
    expect(result.current.showRecheckDialog).toBe(true)
    expect(mockedApi.bulkAction).not.toHaveBeenCalled()

    // count <= 1 -> immediate action
    await act(async () => {
      result.current.prepareRecheckAction(ticks("solo"))
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1].action).toBe("recheck")
  })

  it("prepareRecheckAction pins the single-torrent recheck to its instance so the unified view does not fan out by hash", async () => {
    const { result } = renderActions({ instanceIds: [3, 4, 5] })

    await act(async () => {
      result.current.prepareRecheckAction(pairs([{ instanceId: 3, hash: "shared" }]))
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({
      action: "recheck",
      hashes: [],
      targets: [{ instanceId: 3, hash: "shared" }],
    })
  })

  it("prepareTmmAction keeps the choice, and the confirm sends it for the Action target", async () => {
    const { result } = renderActions({ instanceIds: [3, 4, 5] })

    act(() => {
      result.current.prepareTmmAction(true)
    })
    expect(result.current.showTmmDialog).toBe(true)

    await act(async () => {
      result.current.handleTmmConfirm(pairs([{ instanceId: 3, hash: "shared" }]))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({
      action: "toggleAutoTMM",
      enable: true,
      targets: [{ instanceId: 3, hash: "shared" }],
    })
  })

  it("handleSetSpeedLimits issues only one bulkAction when the download limit is negative", async () => {
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleSetSpeedLimits({ uploadLimit: 500, downloadLimit: -1 }, ticks("h1"))
    })

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({
      action: "setUploadLimit",
      uploadLimit: 500,
    })
  })
})

describe("useTorrentActions - dead query keys", () => {
  it("never refetches or invalidates torrent-counts: no query registers that key", async () => {
    const { result, refetchSpy, invalidateSpy } = renderActions()

    await act(async () => {
      result.current.handleAction("pause", ticks("h1"))
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })

    const touchedKeys = [...refetchSpy.mock.calls, ...invalidateSpy.mock.calls]
      .map(c => (c[0] as { queryKey: unknown[] }).queryKey[0])
    expect(touchedKeys).not.toContain("torrent-counts")
  })
})

describe("useTorrentActions - post-action refetch backstop", () => {
  // Rows outside the SSE live window are only updated by these delayed
  // refetches. The first one can read the backend before its post-action sync
  // has landed (the debounced sync may collapse onto an in-flight pre-action
  // sync), so a single read that lands too early would freeze the row wrong.
  const countListRefetches = (refetchSpy: ReturnType<typeof makeHarness>["refetchSpy"]) =>
    refetchSpy.mock.calls.filter(call => {
      const key = (call[0] as { queryKey?: unknown[] } | undefined)?.queryKey
      return Array.isArray(key) && key[0] === "torrents-list"
    }).length

  it("schedules a follow-up list refetch after the first one", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      result.current.handleAction("pause", ticks("h1"))
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)

    expect(countListRefetches(refetchSpy)).toBe(0)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })

  it("a burst of mutations collapses into one trailing refetch pair (per-instance debounce)", async () => {
    const { result, refetchSpy } = renderActions()

    // Five rapid actions: without debouncing this schedules 10 refetches.
    for (const action of ["pause", "resume", "pause", "resume", "pause"] as const) {
      await act(async () => {
        result.current.handleAction(action, ticks("h1"))
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0)
      })
    }
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(5)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })
    // Only the last action's pair survives: one window refetch + one verification pass.
    expect(countListRefetches(refetchSpy)).toBe(2)
  })

  it("tag updates schedule the follow-up list refetch too", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      await result.current.handleUpdateTags({ add: ["tag1"], remove: [] }, ticks("h1"))
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })

  // The details panel's delete handlers bypass the hook's mutations and call
  // this directly; an immediate refetch there raced the backend's debounced
  // post-delete sync and resurrected deleted rows.
  it("scheduleTorrentListRefetches fires a whole-family refetch pair on the given delay", async () => {
    const { queryClient, refetchSpy } = makeHarness()

    scheduleTorrentListRefetches(queryClient, 2000)

    await vi.advanceTimersByTimeAsync(1999)
    expect(refetchSpy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(1)
    // Whole ["torrents-list"] family: the all-instances view is keyed under
    // instance id 0, which a row's real instance id can never prefix-match.
    expect(refetchSpy.mock.calls[0][0]).toMatchObject({
      queryKey: ["torrents-list"],
      type: "active",
    })

    await vi.advanceTimersByTimeAsync(3000)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })

  it("a fast action does not shorten a slower action's pending verification window", async () => {
    const { queryClient, refetchSpy } = makeHarness()

    // Delete-with-files schedules the 5000/8000 pair...
    scheduleTorrentListRefetches(queryClient, 5000)
    await vi.advanceTimersByTimeAsync(500)
    // ...then a fast action inside that window would replace it with 1000/4000.
    scheduleTorrentListRefetches(queryClient, 1000)

    // The fast action's first refetch runs on its own delay (T+1500)...
    await vi.advanceTimersByTimeAsync(1000)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    // ...but the final pass holds the delete's original T+8000 deadline
    // instead of collapsing to the fast action's T+4500.
    await vi.advanceTimersByTimeAsync(6499)
    expect(refetchSpy).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(refetchSpy).toHaveBeenCalledTimes(2)
  })

  it("torrent rename schedules the follow-up list refetch too", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      await result.current.handleRenameTorrent("h1", "new name")
    })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(750)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })
})
