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
import { makeTorrent } from "@/test/mockTorrent"
import type { TorrentFilters } from "@/types"

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
      await result.current.handleDelete(["h1", "h2"], false)
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

  it("select-all delete sends empty hashes + selectAll, normalizes expanded categories, and uses clientHashes for optimism", async () => {
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
      await result.current.handleDelete(
        ["a", "b"],
        true,
        filters,
        "ubuntu",
        ["zz"],
        { clientHashes: ["a", "b"], totalSelected: 100 }
      )
    })

    const payload = mockedApi.bulkAction.mock.calls[0][1]
    expect(payload.hashes).toEqual([])
    expect(payload.selectAll).toBe(true)
    expect(payload.search).toBe("ubuntu")
    expect(payload.excludeHashes).toEqual(["zz"])
    // expandedCategories collapse into categories in the outgoing payload
    expect(payload.filters?.categories).toEqual(["movies", "tv"])
    expect(payload.filters?.excludeCategories).toEqual(["junk"])

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
      result.current.prepareDeleteAction(["h1"], [makeTorrent({ hash: "h1" })])
    })
    expect(result.current.showDeleteDialog).toBe(true)

    await act(async () => {
      result.current.setDeleteCrossSeeds(true)
    })

    await act(async () => {
      await result.current.handleDelete(["h1"], false)
    })

    expect(result.current.showDeleteDialog).toBe(false)
    expect(result.current.deleteCrossSeeds).toBe(false)
    expect(result.current.contextHashes).toEqual([])
    expect(result.current.contextTorrents).toEqual([])
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
      await result.current.handleDelete(["h1"], false)
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
      await result.current.handleDelete(["h1"], false)
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
      await result.current.handleDelete(["h1", "h2"], false).catch(() => undefined)
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
})

describe("useTorrentActions - tags", () => {
  it("happy path issues sequential removeTags then addTags with comma-joined tags, closes dialog, toasts, and completes", async () => {
    const { result, onActionComplete } = renderActions()

    await act(async () => {
      await result.current.handleUpdateTags(
        { add: ["new1", "new2"], remove: ["old1"] },
        ["h1", "h2"],
        false
      )
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

  it("empty plan short-circuits: no bulkAction call, dialog closed, context cleared", async () => {
    const { result } = renderActions()

    act(() => {
      result.current.prepareTagsAction(["h1"], [makeTorrent({ hash: "h1" })])
    })
    expect(result.current.showTagsDialog).toBe(true)

    await act(async () => {
      await result.current.handleUpdateTags({ add: [], remove: [] }, ["h1"], false)
    })

    expect(mockedApi.bulkAction).not.toHaveBeenCalled()
    expect(result.current.showTagsDialog).toBe(false)
    expect(result.current.contextHashes).toEqual([])
  })

  it("ERROR PATH (partial failure): removeTags resolves, addTags rejects -> partialTags toast, dialog closed, context cleared", async () => {
    const { result } = renderActions()

    // Open the dialog and seed context so we can prove the onError
    // TagBulkActionError branch (not the mutationFn) tears them down.
    act(() => {
      result.current.prepareTagsAction(["h1"], [makeTorrent({ hash: "h1" })])
    })
    expect(result.current.showTagsDialog).toBe(true)
    expect(result.current.contextHashes).toEqual(["h1"])

    // First call (removeTags) succeeds, second call (addTags) rejects.
    mockedApi.bulkAction
      .mockResolvedValueOnce(undefined as never)
      .mockRejectedValueOnce(new Error("add failed"))

    await act(async () => {
      await result.current
        .handleUpdateTags({ add: ["new1"], remove: ["old1"] }, ["h1"], false)
        .catch(() => undefined)
    })

    // partialTags.title (count=1), not updateTagsFailed
    const errorKeys = mockedToast.error.mock.calls.map(c => c[0])
    expect(errorKeys).toContain("actionToasts.partialTags.title/1")
    expect(errorKeys.some(k => String(k).startsWith("actionToasts.updateTagsFailed"))).toBe(false)

    // onError TagBulkActionError branch closes the dialog and clears context.
    expect(result.current.showTagsDialog).toBe(false)
    expect(result.current.contextHashes).toEqual([])
    expect(result.current.contextTorrents).toEqual([])
  })
})

describe("useTorrentActions - non-delete success transitions", () => {
  it("resume uses the 2000ms refetch branch", async () => {
    const { result, refetchSpy } = renderActions()

    await act(async () => {
      result.current.handleAction("resume", ["h1"])
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
      await result.current.handleSetLocation("/data/new", ["h1"], false)
    })

    // Dialog closes and context clears on success.
    expect(result.current.showLocationDialog).toBe(false)
    expect(result.current.contextHashes).toEqual([])
    expect(result.current.contextTorrents).toEqual([])
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
      result.current.prepareRecheckAction(["h1", "h2"], 2)
    })
    expect(result.current.showRecheckDialog).toBe(true)
    expect(mockedApi.bulkAction).not.toHaveBeenCalled()

    // count <= 1 -> immediate action
    await act(async () => {
      result.current.prepareRecheckAction(["solo"], 1)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1].action).toBe("recheck")
  })

  it("prepareRecheckAction pins the single-torrent recheck to its instance so the unified view does not fan out by hash", async () => {
    const { result } = renderActions({ instanceIds: [3, 4, 5] })
    const torrent = { ...makeTorrent({ hash: "shared" }), instanceId: 3 }

    await act(async () => {
      result.current.prepareRecheckAction(["shared"], 1, [torrent])
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(1)
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({
      action: "recheck",
      hashes: ["shared"],
      targets: [{ instanceId: 3, hash: "shared" }],
    })
  })

  it("prepareRecheckAction stores the torrents in contextTorrents for the confirm dialog", async () => {
    const { result } = renderActions({ instanceIds: [3, 4, 5] })
    const torrents = [
      { ...makeTorrent({ hash: "a" }), instanceId: 3 },
      { ...makeTorrent({ hash: "b" }), instanceId: 4 },
    ]

    act(() => {
      result.current.prepareRecheckAction(["a", "b"], 2, torrents)
    })
    expect(result.current.showRecheckDialog).toBe(true)
    expect(result.current.contextTorrents).toEqual(torrents)
  })

  it("handleSetSpeedLimits issues only one bulkAction when the download limit is negative", async () => {
    const { result } = renderActions()

    await act(async () => {
      await result.current.handleSetSpeedLimits(500, -1, ["h1"], false)
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
      result.current.handleAction("pause", ["h1"])
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
      result.current.handleAction("pause", ["h1"])
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
        result.current.handleAction(action, ["h1"])
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
      await result.current.handleUpdateTags({ add: ["tag1"], remove: [] }, ["h1"])
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
