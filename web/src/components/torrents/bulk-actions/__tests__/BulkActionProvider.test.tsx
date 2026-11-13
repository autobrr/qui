/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: { bulkAction: vi.fn(), getDeleteCrossSeeds: vi.fn(), addCrossSeedBlocklist: vi.fn(), renameTorrent: vi.fn(), getTorrents: vi.fn() },
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}))

// Each dialog with values shows the values that the module gives it, and its
// confirm button sends the values of the test. The confirmations stay real.
const stubs = vi.hoisted(() => ({ confirmArgs: {} as Record<string, unknown[]> }))
vi.mock("@/components/torrents/TorrentDialogs", async (importOriginal) => {
  const { createElement } = await import("react")
  type StubProps = Record<string, unknown> & { open: boolean; onConfirm: (...args: unknown[]) => void; onOpenChange: (open: boolean) => void }
  const stub = (name: string, show: (props: StubProps) => unknown[]) => (props: StubProps) => props.open ? createElement("div", { role: "dialog" },
    createElement("h2", null, name),
    createElement("p", null, show(props).map(value => JSON.stringify(value)).join(" ")),
    createElement("button", { onClick: () => props.onConfirm(...(stubs.confirmArgs[name] ?? [])) }, `confirm:${name}`)
  ) : null
  return {
    ...(await importOriginal<typeof import("@/components/torrents/TorrentDialogs")>()),
    TagEditorDialog: stub("tags", p => [p.hashCount, p.availableTags, p.isLoadingTags]),
    SetCategoryDialog: stub("category", p => [p.hashCount, Object.keys(p.availableCategories as object), p.isLoadingCategories, p.useSubcategories, p.initialCategory]),
    SetLocationDialog: stub("location", p => [p.hashCount, p.instanceId, p.capabilities, p.initialLocation]),
    ShareLimitDialog: stub("shareLimit", p => [p.hashCount, p.instanceIds]),
    SpeedLimitsDialog: stub("speedLimits", p => [p.hashCount]),
    SetCommentDialog: stub("comment", p => [p.hashCount, p.instanceId, p.torrentHash]),
    RenameTorrentDialog: stub("rename", p => [p.currentName]),
  }
})

// Keeps the count and the size in the text, so a test can read them in a toast or a dialog.
// One translator for every render: the tag editor runs an effect again when t changes.
const translation = vi.hoisted(() => ({
  t: (key: string, opts?: Record<string, unknown>) => opts && "count" in opts ? `${key}/${opts.count}` : opts && "size" in opts ? `${key}/${opts.size}` : opts && "name" in opts ? `${key}/${opts.name}` : key,
}))
vi.mock("react-i18next", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-i18next")>()),
  useTranslation: () => translation,
}))

// The size formatter of the delete dialog reads its unit labels from the i18n resources.
import "@/i18n"

import { BulkActionProvider } from "../BulkActionProvider"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useBulkActions } from "../bulk-actions-context"
import { TorrentSelectionProvider, useTorrentSelectionContext, type TorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { api } from "@/lib/api"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { resetPendingListRefetchesForTests } from "../bulk-action-requests"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import { EMPTY_ACTION_TARGET, hasSelection, type ActionTarget, type SelectAllTarget } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"
import { LIST_FACTS } from "@/test/mockListFacts"
import { makeTorrent } from "@/test/mockTorrent"
import { formatBytes } from "@/lib/utils"
import type { LocalCrossSeedMatch, Torrent } from "@/types"
import { toast } from "sonner"

const mockedApi = vi.mocked(api, true)
const mockedToast = vi.mocked(toast, true)

const INSTANCE_ID = 7

function makeHarness() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const refetchSpy = vi.spyOn(queryClient, "refetchQueries").mockResolvedValue(undefined)
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue(undefined)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <TorrentSelectionProvider>
          <BulkActionProvider>{children}</BulkActionProvider>
        </TorrentSelectionProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
  return { queryClient, wrapper, refetchSpy, invalidateSpy }
}

// A Layout that publishes, and an Action surface that picks.
function renderModule(published?: Partial<TorrentSelectionSummary> & { actionTarget: ActionTarget }) {
  const harness = makeHarness()
  const { result } = renderHook(() => {
    const { selection, dispatch, publishSummary } = useTorrentSelectionContext()
    return { ...useBulkActions(), publish: publishSummary, selection, dispatchSelection: dispatch }
  }, { wrapper: harness.wrapper })
  const publish = (value: Partial<TorrentSelectionSummary> & { actionTarget: ActionTarget }) => act(() => {
    result.current.publish({ instanceId: INSTANCE_ID, totalSize: 0, excludedSize: 0, ...value, listFacts: value.listFacts ?? LIST_FACTS })
  })
  if (published) publish(published)
  return { ...harness, result, publish }
}

function ticks(...hashes: string[]): ActionTarget {
  return { kind: "ticks", hashes, count: hashes.length, previewTorrents: hashes.map(hash => makeTorrent({ hash })) }
}

function pairs(targets: TorrentActionTarget[], previewTorrents: Torrent[] = []): ActionTarget {
  return { kind: "ticks", hashes: [], targets, count: targets.length, previewTorrents }
}

function selectAll(count: number, part: Partial<SelectAllTarget> = {}, loaded: string[] = []): ActionTarget {
  return { kind: "selectAll", selectAll: { filters: makeFilters(), search: "", ...part }, count, previewTorrents: loaded.map(hash => makeTorrent({ hash })) }
}

async function flush() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

function dialogTitle() {
  return document.body.querySelector("[role=\"dialog\"] h2")?.textContent ?? null
}

function clickButton(text: string) {
  const button = Array.from(document.body.querySelectorAll("button")).find(b => b.textContent === text)
  if (!button) throw new Error(`no button ${text}`)
  fireEvent.click(button)
}

const countListRefetches = (refetchSpy: ReturnType<typeof makeHarness>["refetchSpy"]) =>
  refetchSpy.mock.calls.filter(call => {
    const key = (call[0] as { queryKey?: unknown[] } | undefined)?.queryKey
    return Array.isArray(key) && key[0] === "torrents-list"
  }).length

beforeEach(() => {
  stubs.confirmArgs = {}
  vi.useFakeTimers()
  resetPendingListRefetchesForTests()
  localStorage.clear()
  mockedApi.bulkAction.mockResolvedValue(undefined as never)
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe("bulk action module - requests", () => {
  it("sends a pick for the published Action target to the instance of the page", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1", "h2"), instanceIds: [INSTANCE_ID] })

    act(() => result.current.pick({ action: "pause" }))
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "pause", hashes: ["h1", "h2"], selectAll: false, instanceIds: [INSTANCE_ID] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.pause/2")
  })

  it("sends the direction of a toggle and names it in the toast", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "toggleSequentialDownload", enable: false }))
    await flush()

    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({ action: "toggleSequentialDownload", enable: false })
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.toggleSequentialDownloadDisable/1")
  })

  it("names the action and the count of the request in a failure toast", async () => {
    mockedApi.bulkAction.mockRejectedValueOnce(new Error("qbit down"))
    const { result } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "topPriority" }))
    await flush()

    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.topPriority/2", { description: "qbit down" })
  })

  it("resume uses the 2000ms refetch branch", async () => {
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "resume" }))
    await flush()

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

  it("never refetches or invalidates torrent-counts: no query registers that key", async () => {
    const { result, refetchSpy, invalidateSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "pause" }))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })

    const touchedKeys = [...refetchSpy.mock.calls, ...invalidateSpy.mock.calls]
      .map(c => (c[0] as { queryKey: unknown[] }).queryKey[0])
    expect(touchedKeys).not.toContain("torrent-counts")
  })

  // Rows outside the SSE live window are only updated by these delayed
  // refetches. The first one can read the backend before its post-action sync
  // has landed, so a single read that lands too early would freeze the row wrong.
  it("schedules a follow-up list refetch after the first one", async () => {
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "pause" }))
    await flush()
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

  it("a burst of picks collapses into one trailing refetch pair", async () => {
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    for (const action of ["pause", "resume", "pause", "resume", "pause"] as const) {
      act(() => result.current.pick({ action }))
      await flush()
    }
    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(5)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })
})

describe("bulk action module - confirmations", () => {
  it("asks before a Recheck of more than 1 torrent, and runs a Recheck of 1 torrent at once", async () => {
    const { result, publish } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "recheck" }))
    await flush()
    expect(dialogTitle()).toBe("recheckDialog.title/2")
    expect(mockedApi.bulkAction).not.toHaveBeenCalled()

    clickButton("recheckDialog.confirm")
    await flush()
    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "recheck", hashes: ["h1", "h2"] })]])
    expect(dialogTitle()).toBeNull()

    publish({ actionTarget: ticks("solo") })
    act(() => result.current.pick({ action: "reannounce" }))
    await flush()
    expect(dialogTitle()).toBeNull()
    expect(mockedApi.bulkAction.mock.calls[1]).toEqual([INSTANCE_ID, expect.objectContaining({ action: "reannounce", hashes: ["solo"] })])
  })

  it("pins a Recheck of 1 torrent to its instance so the Unified view does not fan out by hash", async () => {
    const { result } = renderModule({ instanceId: 0, instanceIds: [3, 4, 5], actionTarget: pairs([{ instanceId: 3, hash: "shared" }]) })

    act(() => result.current.pick({ action: "recheck" }))
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[0, expect.objectContaining({ action: "recheck", hashes: [], targets: [{ instanceId: 3, hash: "shared" }] })]])
  })

  it("keeps the direction of automatic management, and the confirm sends it for the Action target", async () => {
    const { result } = renderModule({ instanceId: 0, instanceIds: [3, 4, 5], actionTarget: pairs([{ instanceId: 3, hash: "shared" }]) })

    act(() => result.current.pick({ action: "toggleAutoTMM", enable: true }))
    await flush()
    expect(mockedApi.bulkAction).not.toHaveBeenCalled()

    clickButton("dialogs.tmm.enable")
    await flush()
    expect(mockedApi.bulkAction.mock.calls).toEqual([[0, expect.objectContaining({ action: "toggleAutoTMM", enable: true, targets: [{ instanceId: 3, hash: "shared" }] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.toggleAutoTMMEnable/1")
  })

  it("shows the live count in an open confirmation, and the confirm sends the live target", async () => {
    const { result, publish } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "reannounce" }))
    publish({ actionTarget: ticks("h1", "h2", "h3") })
    expect(dialogTitle()).toBe("reannounceDialog.title/3")

    clickButton("reannounceDialog.confirm")
    await flush()
    expect(mockedApi.bulkAction.mock.calls[0][1]).toMatchObject({ action: "reannounce", hashes: ["h1", "h2", "h3"] })
  })

  it("leaves the confirmation open when the request fails", async () => {
    mockedApi.bulkAction.mockRejectedValueOnce(new Error("qbit down"))
    const { result } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "recheck" }))
    clickButton("recheckDialog.confirm")
    await flush()

    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.recheck/2", { description: "qbit down" })
    expect(dialogTitle()).toBe("recheckDialog.title/2")
  })

  it("closes an open confirmation when the Layout publishes nothing", () => {
    const { result } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "recheck" }))
    expect(dialogTitle()).toBe("recheckDialog.title/2")

    act(() => result.current.publish(null))
    expect(dialogTitle()).toBeNull()
  })

  // regression: #3983
  it("closes an open confirmation when the selection becomes empty", () => {
    const { result, publish } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "recheck" }))
    expect(dialogTitle()).toBe("recheckDialog.title/2")

    publish({ actionTarget: EMPTY_ACTION_TARGET })
    expect(dialogTitle()).toBeNull()
  })

  it("sends a select-all request with the scope and the Exclusions", async () => {
    const filters = makeFilters({ categories: ["movies"] })
    const actionTarget: ActionTarget = { kind: "selectAll", selectAll: { filters, search: "linux", excludeHashes: ["skip"] }, count: 40, previewTorrents: [] }
    const { result } = renderModule({ actionTarget })

    act(() => result.current.pick({ action: "recheck" }))
    expect(dialogTitle()).toBe("recheckDialog.title/40")
    clickButton("recheckDialog.confirm")
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "recheck", hashes: [], selectAll: true, search: "linux", excludeHashes: ["skip"] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.recheck/40")
  })
})

function deleteTitle() {
  return document.body.querySelector("[role=\"alertdialog\"] h2")?.textContent ?? null
}

function checkbox(id: string) {
  const box = document.getElementById(id)
  if (!box) throw new Error(`no checkbox ${id}`)
  return box
}

function crossSeedMatch(overrides: Partial<LocalCrossSeedMatch>): LocalCrossSeedMatch {
  return {
    instanceId: INSTANCE_ID,
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

// Checks for cross-seeds in the open delete dialog, and ticks "also delete cross-seeds" when asked.
async function findCrossSeeds(matches: LocalCrossSeedMatch[], { tick = true } = {}) {
  mockedApi.getDeleteCrossSeeds.mockResolvedValue({ matches, uncheckedInstances: [] })
  clickButton("crossSeedWarning.idle.check")
  await flush()
  if (tick) fireEvent.click(checkbox("deleteCrossSeeds"))
}

async function confirmDelete(label = "deleteDialog.delete") {
  clickButton(label)
  await flush()
  await flush()
}

const TORRENTS_LIST_KEY = ["torrents-list", INSTANCE_ID, "page-1"]

type CachedList = { torrents: { hash: string }[]; total: number; totalCount: number }

describe("bulk action module - delete", () => {
  it("deletes the Action target, removes its rows from the cached list, clears the selection, and closes the dialog", async () => {
    const { result, queryClient } = renderModule({ actionTarget: ticks("h1", "h2") })
    act(() => result.current.dispatchSelection({ type: "tick", keys: ["h1", "h2"], checked: true }))
    queryClient.setQueryData(TORRENTS_LIST_KEY, {
      torrents: [makeTorrent({ hash: "h1" }), makeTorrent({ hash: "h2" }), makeTorrent({ hash: "h3" })],
      total: 3,
      totalCount: 10,
    })

    act(() => result.current.pick({ action: "delete" }))
    expect(deleteTitle()).toBe("deleteDialog.title/2")
    await confirmDelete()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "delete", hashes: ["h1", "h2"], selectAll: false, deleteFiles: false })]])
    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY) as CachedList
    expect(cache.torrents.map(t => t.hash)).toEqual(["h3"])
    expect(cache.total).toBe(1)
    expect(cache.totalCount).toBe(8)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/2")
    expect(hasSelection(result.current.selection)).toBe(false)
    expect(deleteTitle()).toBeNull()
  })

  it("sends the Select all scope with the filters, and removes the loaded rows of the target", async () => {
    const filters = makeFilters({ categories: ["raw"] })
    const { result, queryClient } = renderModule({ actionTarget: selectAll(100, { filters, search: "ubuntu", excludeHashes: ["zz"] }, ["a", "b"]) })
    queryClient.setQueryData(TORRENTS_LIST_KEY, {
      torrents: [makeTorrent({ hash: "a" }), makeTorrent({ hash: "b" }), makeTorrent({ hash: "c" })],
      total: 100,
      totalCount: 100,
    })

    act(() => result.current.pick({ action: "delete" }))
    expect(deleteTitle()).toBe("deleteDialog.titleSelectAll/100")
    await confirmDelete("deleteDialog.deleteCount/100")

    const [, body] = mockedApi.bulkAction.mock.calls[0]
    expect(body).toMatchObject({ hashes: [], selectAll: true, search: "ubuntu", excludeHashes: ["zz"] })
    expect(body.filters).toBe(filters)
    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY) as CachedList
    expect(cache.torrents.map(t => t.hash)).toEqual(["c"])
    expect(cache.totalCount).toBe(0)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/100")
  })

  it("shows the size of the filter minus the loaded Exclusions", () => {
    const { result } = renderModule({ actionTarget: selectAll(100), totalSize: 3 * 1024 ** 3, excludedSize: 1024 ** 3 })

    act(() => result.current.pick({ action: "delete" }))

    expect(document.body.textContent).toContain(`deleteDialog.totalSize/${formatBytes(2 * 1024 ** 3)}`)
  })

  it("uses the deleteWithFiles toast and the 5000ms refetch when the delete-files preference is locked on", async () => {
    localStorage.setItem("qui-delete-files-lock", JSON.stringify(true))
    localStorage.setItem("qui-delete-files-default", JSON.stringify(true))
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete()

    expect(mockedApi.bulkAction.mock.calls[0][1].deleteFiles).toBe(true)
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.deleteWithFiles/1")
    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4999)
    })
    expect(countListRefetches(refetchSpy)).toBe(0)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)
  })

  it("uses the 2000ms refetch when the user keeps the files", async () => {
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete()

    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1999)
    })
    expect(countListRefetches(refetchSpy)).toBe(0)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)
  })

  it("leaves the dialog open, the cache and the selection as they are when the delete fails", async () => {
    mockedApi.bulkAction.mockRejectedValueOnce(new Error("boom"))
    const { result, queryClient } = renderModule({ actionTarget: ticks("h1", "h2") })
    act(() => result.current.dispatchSelection({ type: "tick", keys: ["h1", "h2"], checked: true }))
    queryClient.setQueryData(TORRENTS_LIST_KEY, { torrents: [makeTorrent({ hash: "h1" }), makeTorrent({ hash: "h2" })], total: 2, totalCount: 2 })

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete()

    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.delete/2", { description: "boom" })
    expect((queryClient.getQueryData(TORRENTS_LIST_KEY) as CachedList).torrents.map(t => t.hash)).toEqual(["h1", "h2"])
    expect(hasSelection(result.current.selection)).toBe(true)
    expect(deleteTitle()).toBe("deleteDialog.title/2")
  })

  // regression: #3989
  it("keeps the dialog open while the delete runs, so Cancel and Escape do not suggest that they stop it", async () => {
    let finish = () => {}
    mockedApi.bulkAction.mockReturnValueOnce(new Promise(resolve => {
      finish = () => resolve(undefined as never)
    }))
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete()
    clickButton("deleteDialog.cancel")
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
    await flush()
    expect(deleteTitle()).toBe("deleteDialog.title/1")

    finish()
    await flush()
    expect(deleteTitle()).toBeNull()
  })

  // regression: #3989
  it("locks the delete options while the delete runs, so the dialog shows the choices that the request carries", async () => {
    let finish = () => {}
    mockedApi.bulkAction.mockReturnValueOnce(new Promise(resolve => {
      finish = () => resolve(undefined as never)
    }))
    const actionTarget: ActionTarget = { kind: "ticks", hashes: ["a"], count: 1, previewTorrents: [makeTorrent({ hash: "a", tags: "cross-seed" })] }
    const { result } = renderModule({ actionTarget })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })])
    await confirmDelete()
    const options = ["deleteFiles", "deleteCrossSeeds", "blockCrossSeeds"]
    options.forEach(id => fireEvent.click(checkbox(id)))

    expect(options.map(id => checkbox(id).getAttribute("aria-checked"))).toEqual(["false", "true", "false"])

    finish()
    await flush()
    expect(deleteTitle()).toBeNull()
  })

  it("starts each open with \"also delete cross-seeds\" cleared and no cross-seed result", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })])
    expect(checkbox("deleteCrossSeeds").getAttribute("aria-checked")).toBe("true")
    clickButton("deleteDialog.cancel")
    await flush()

    act(() => result.current.pick({ action: "delete" }))
    expect(document.getElementById("deleteCrossSeeds")).toBeNull()
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })], { tick: false })
    expect(checkbox("deleteCrossSeeds").getAttribute("aria-checked")).toBe("false")
  })

  it("lists the chosen cross-seeds in the same request, as plain hashes on one instance", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })])
    await confirmDelete()

    expect(mockedApi.getDeleteCrossSeeds).toHaveBeenCalledWith(INSTANCE_ID, { targets: [{ instanceId: INSTANCE_ID, hash: "h1" }] }, expect.any(AbortSignal))
    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ hashes: ["h1", "cs1"], targets: undefined })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.delete/2")
  })

  it("lists the chosen cross-seeds as pairs, and no bare hash, on the cross-instance endpoint", async () => {
    const { result } = renderModule({ actionTarget: pairs([{ instanceId: INSTANCE_ID, hash: "h1" }], [makeTorrent({ hash: "h1" })]) })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })])
    await confirmDelete()

    const [, body] = mockedApi.bulkAction.mock.calls[0]
    expect(body.hashes).toEqual([])
    expect(body.targets).toEqual([{ instanceId: INSTANCE_ID, hash: "h1" }, { instanceId: INSTANCE_ID, hash: "cs1" }])
  })

  it("checks the Select all scope, and deletes the chosen cross-seeds in a second request with the same delete-files choice", async () => {
    localStorage.setItem("qui-delete-files-lock", JSON.stringify(true))
    localStorage.setItem("qui-delete-files-default", JSON.stringify(true))
    const scope = { filters: makeFilters(), search: "", excludeHashes: ["cs1"] }
    const { result } = renderModule({ actionTarget: selectAll(50, scope, ["a"]) })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" }), crossSeedMatch({ instanceId: 9, hash: "cs2" })])
    await confirmDelete("deleteDialog.deleteCount/52")

    expect(mockedApi.getDeleteCrossSeeds).toHaveBeenCalledWith(INSTANCE_ID, { selectAll: true, ...scope }, expect.any(AbortSignal))
    expect(mockedApi.bulkAction.mock.calls).toEqual([
      [INSTANCE_ID, expect.objectContaining({ hashes: [], selectAll: true, excludeHashes: ["cs1"], deleteFiles: true })],
      [0, { action: "delete", deleteFiles: true, instanceIds: undefined, hashes: [], targets: [{ instanceId: INSTANCE_ID, hash: "cs1" }, { instanceId: 9, hash: "cs2" }] }],
    ])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.deleteWithFiles/50")
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.deleteWithFiles/2")
  })

  // A request on one instance leaves out targets on other instances.
  // regression: #3592
  it("sends the chosen cross-seeds to the all-instances scope when one is on another instance", async () => {
    const { result, queryClient } = renderModule({ actionTarget: ticks("h1") })
    queryClient.setQueryData(TORRENTS_LIST_KEY, {
      torrents: [makeTorrent({ hash: "h1" }), makeTorrent({ hash: "cs1" }), makeTorrent({ hash: "keep" })],
      total: 10,
      totalCount: 10,
    })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" }), crossSeedMatch({ instanceId: 9, hash: "cs2" })])
    await confirmDelete()

    expect(mockedApi.bulkAction.mock.calls).toEqual([
      [INSTANCE_ID, expect.objectContaining({ hashes: ["h1"] })],
      [0, expect.objectContaining({ hashes: [], targets: [{ instanceId: INSTANCE_ID, hash: "cs1" }, { instanceId: 9, hash: "cs2" }] })],
    ])
    // The cross-seed on instance 9 is not in the list of this instance.
    const cache = queryClient.getQueryData(TORRENTS_LIST_KEY) as CachedList
    expect(cache.torrents.map(t => t.hash)).toEqual(["keep"])
    expect(cache.totalCount).toBe(8)
  })

  // A retry would send the Select all delete again, and delete each torrent that joined the filter since.
  it("closes the dialog and clears the selection when only the cross-seed request fails", async () => {
    mockedApi.bulkAction.mockResolvedValueOnce(undefined as never).mockRejectedValueOnce(new Error("boom"))
    const { result } = renderModule({ actionTarget: selectAll(50, {}, ["a"]) })
    act(() => result.current.dispatchSelection({ type: "selectAll" }))

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ hash: "cs1" })])
    await confirmDelete("deleteDialog.deleteCount/51")

    expect(mockedApi.bulkAction).toHaveBeenCalledTimes(2)
    expect(mockedToast.error).toHaveBeenCalledTimes(1)
    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.delete/1", { description: "boom" })
    expect(hasSelection(result.current.selection)).toBe(false)
    expect(deleteTitle()).toBeNull()
  })

  // regression: #3989
  it("closes the dialog and clears the selection when a Select all delete fails in the Unified view", async () => {
    mockedApi.bulkAction.mockRejectedValueOnce(new Error("boom"))
    const { result } = renderModule({ instanceId: ALL_INSTANCES_ID, actionTarget: selectAll(50, {}, ["a"]) })
    act(() => result.current.dispatchSelection({ type: "selectAll" }))

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete("deleteDialog.deleteCount/50")

    expect(mockedApi.bulkAction.mock.calls[0][0]).toBe(ALL_INSTANCES_ID)
    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.delete/1", { description: "boom" })
    expect(hasSelection(result.current.selection)).toBe(false)
    expect(deleteTitle()).toBeNull()
  })

  // The server can delete on one instance before the request fails on the next one.
  // regression: #3989
  it("refetches the list when a delete fails in the Unified view, for Select all and for ticked torrents", async () => {
    const targets: ActionTarget[] = [selectAll(50, {}, ["a"]), pairs([{ instanceId: INSTANCE_ID, hash: "h1" }, { instanceId: 9, hash: "h2" }])]
    for (const actionTarget of targets) {
      mockedApi.bulkAction.mockRejectedValueOnce(new Error("boom"))
      const { result, refetchSpy } = renderModule({ instanceId: ALL_INSTANCES_ID, actionTarget })

      act(() => result.current.pick({ action: "delete" }))
      await confirmDelete(actionTarget.kind === "selectAll" ? "deleteDialog.deleteCount/50" : "deleteDialog.delete")
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2000)
      })

      expect(countListRefetches(refetchSpy)).toBe(1)
      cleanup()
    }
  })

  it("blocks the tagged torrent before the delete, only when the user asks", async () => {
    const order: string[] = []
    mockedApi.addCrossSeedBlocklist.mockImplementation(async () => {
      order.push("block")
      return {} as Awaited<ReturnType<typeof api.addCrossSeedBlocklist>>
    })
    mockedApi.bulkAction.mockImplementation(async (_id, body) => {
      order.push(body.action)
    })
    const actionTarget: ActionTarget = { kind: "ticks", hashes: ["a"], count: 1, previewTorrents: [makeTorrent({ hash: "a", tags: "cross-seed" })] }
    const { result } = renderModule({ actionTarget })

    act(() => result.current.pick({ action: "delete" }))
    await confirmDelete()
    expect(order).toEqual(["delete"])

    order.length = 0
    act(() => result.current.pick({ action: "delete" }))
    fireEvent.click(checkbox("blockCrossSeeds"))
    await confirmDelete()
    expect(order).toEqual(["block", "delete"])
    expect(mockedApi.addCrossSeedBlocklist).toHaveBeenCalledWith({ instanceId: INSTANCE_ID, infoHash: "a" })
  })

  it("offers no block for a target without the tag", () => {
    const { result } = renderModule({ actionTarget: ticks("a") })

    act(() => result.current.pick({ action: "delete" }))

    expect(document.getElementById("blockCrossSeeds")).toBeNull()
  })

  it("offers the block for a tagged cross-seed, and blocks no untagged target", async () => {
    localStorage.setItem(`qui-cross-seed-blocklist-${INSTANCE_ID}`, JSON.stringify(true))
    const { result } = renderModule({ actionTarget: ticks("a") })

    act(() => result.current.pick({ action: "delete" }))
    expect(document.getElementById("blockCrossSeeds")).toBeNull()
    await findCrossSeeds([crossSeedMatch({ instanceId: 9, hash: "a", tags: "cross-seed" })], { tick: false })
    expect(checkbox("blockCrossSeeds").getAttribute("aria-checked")).toBe("true")
    await confirmDelete()

    expect(mockedApi.addCrossSeedBlocklist).not.toHaveBeenCalled()
  })

  it("blocks no torrent on another instance that the delete keeps", async () => {
    localStorage.setItem(`qui-cross-seed-blocklist-${INSTANCE_ID}`, JSON.stringify(true))
    const actionTarget: ActionTarget = { kind: "ticks", hashes: ["a"], count: 1, previewTorrents: [makeTorrent({ hash: "a", tags: "cross-seed" })] }
    const { result } = renderModule({ actionTarget })

    act(() => result.current.pick({ action: "delete" }))
    await findCrossSeeds([crossSeedMatch({ instanceId: 9, hash: "a", tags: "cross-seed" })], { tick: false })
    await confirmDelete()

    expect(mockedApi.addCrossSeedBlocklist.mock.calls).toEqual([[{ instanceId: INSTANCE_ID, infoHash: "a" }]])
  })
})

// The values that a stub dialog shows, in the order of its stub.
function dialogValues(name: string) {
  const title = Array.from(document.body.querySelectorAll("[role=\"dialog\"] h2")).find(h => h.textContent === name)
  return title?.nextElementSibling?.textContent ?? null
}

function confirmDialog(name: string, ...args: unknown[]) {
  stubs.confirmArgs[name] = args
  clickButton(`confirm:${name}`)
}

const CROSS_INSTANCE_FACTS = { ...LIST_FACTS, isCrossInstance: true, categories: { movies: { name: "movies", savePath: "/m" }, tv: { name: "tv", savePath: "/t" } }, tags: ["one", "two"], allowSubcategories: true }

describe("bulk action module - dialogs with values", () => {
  it("updates the tags in two steps, the removal first, then closes and names the count", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "setTags" }))
    expect(dialogTitle()).toBe("tags")
    confirmDialog("tags", { add: ["new1", "new2"], remove: ["old1"] })
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([
      [INSTANCE_ID, expect.objectContaining({ action: "removeTags", tags: "old1", hashes: ["h1", "h2"], selectAll: false })],
      [INSTANCE_ID, expect.objectContaining({ action: "addTags", tags: "new1,new2", hashes: ["h1", "h2"], selectAll: false })],
    ])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.updatedTags/2")
    expect(dialogTitle()).toBeNull()
  })

  it("closes the tag editor with no request when the plan is empty", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setTags" }))
    confirmDialog("tags", { add: [], remove: [] })
    await flush()

    expect(mockedApi.bulkAction).not.toHaveBeenCalled()
    expect(dialogTitle()).toBeNull()
  })

  it("closes the tag editor after a partial failure, and names the step that failed", async () => {
    mockedApi.bulkAction.mockResolvedValueOnce(undefined as never).mockRejectedValueOnce(new Error("add failed"))
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setTags" }))
    confirmDialog("tags", { add: ["new1"], remove: ["old1"] })
    await flush()

    const errorKeys = mockedToast.error.mock.calls.map(call => call[0])
    expect(errorKeys).toEqual(["actionToasts.partialTags.title/1"])
    expect(dialogTitle()).toBeNull()
  })

  it("sends the Select all scope, not the loaded rows, for the tags", async () => {
    const filters = makeFilters({ categories: ["movies"] })
    const { result } = renderModule({ actionTarget: selectAll(40, { filters, search: "linux", excludeHashes: ["b"] }, ["a"]) })

    act(() => result.current.pick({ action: "setTags" }))
    confirmDialog("tags", { add: ["keep"], remove: [] })
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "addTags", hashes: [], selectAll: true, search: "linux", excludeHashes: ["b"] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.updatedTags/40")
  })

  it("schedules the follow-up list refetch after a tag update", async () => {
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setTags" }))
    confirmDialog("tags", { add: ["tag1"], remove: [] })
    await flush()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })

  it("lists each category and each tag of the list response in the Unified view", () => {
    const { result } = renderModule({ instanceId: ALL_INSTANCES_ID, actionTarget: pairs([{ instanceId: 3, hash: "a" }]), listFacts: CROSS_INSTANCE_FACTS })

    act(() => result.current.pick({ action: "setCategory" }))
    expect(dialogValues("category")).toBe("1 [\"movies\",\"tv\"] false true \"\"")
    act(() => result.current.pick({ action: "setTags" }))
    expect(dialogValues("tags")).toBe("1 [\"one\",\"two\"] false")
  })

  it("shows the loading state on a single instance until the metadata loads", () => {
    const { result } = renderModule({ actionTarget: ticks("h1"), listFacts: { ...CROSS_INSTANCE_FACTS, isCrossInstance: false } })

    act(() => result.current.pick({ action: "setCategory" }))
    expect(dialogValues("category")).toBe("1 [] true true \"\"")
  })

  it("sets the category of the dialog, closes on success, and stays open on a failure", async () => {
    mockedApi.bulkAction.mockRejectedValueOnce(new Error("qbit down"))
    const { result } = renderModule({ actionTarget: ticks("h1", "h2") })

    act(() => result.current.pick({ action: "setCategory" }))
    confirmDialog("category", "movies")
    await flush()
    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.setCategory/2", { description: "qbit down" })
    expect(dialogTitle()).toBe("category")

    confirmDialog("category", "movies")
    await flush()
    expect(mockedApi.bulkAction.mock.calls[1]).toEqual([INSTANCE_ID, expect.objectContaining({ action: "setCategory", category: "movies", hashes: ["h1", "h2"] })])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setCategory/2")
    expect(dialogTitle()).toBeNull()
  })

  it("sets the category of the submenu at once", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setCategory", category: "" }))
    await flush()

    expect(dialogTitle()).toBeNull()
    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "setCategory", category: "", hashes: ["h1"] })]])
  })

  it("opens the warning before the location dialog, then sets the location with the 1000ms refetch", async () => {
    const capabilities = { supportsSetLocation: true } as unknown as NonNullable<typeof LIST_FACTS.capabilities>
    const { result, refetchSpy } = renderModule({ actionTarget: ticks("h1"), listFacts: { ...LIST_FACTS, capabilities } })

    act(() => result.current.pick({ action: "setLocation" }))
    expect(dialogTitle()).toBe("dialogs.locationWarning.title/1")
    expect(mockedApi.bulkAction).not.toHaveBeenCalled()
    clickButton("dialogs.locationWarning.continue")
    expect(dialogValues("location")).toBe(`1 ${INSTANCE_ID} {"supportsSetLocation":true} "/downloads"`)

    confirmDialog("location", "/data/new")
    await flush()
    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "setLocation", location: "/data/new", hashes: ["h1"] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setLocation/1")
    expect(dialogTitle()).toBeNull()

    refetchSpy.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(999)
    })
    expect(countListRefetches(refetchSpy)).toBe(0)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)
  })

  it("sends the share limits", async () => {
    const { result } = renderModule({ instanceId: ALL_INSTANCES_ID, actionTarget: pairs([{ instanceId: 3, hash: "a" }, { instanceId: 4, hash: "b" }]) })

    act(() => result.current.pick({ action: "setShareLimit" }))
    expect(dialogValues("shareLimit")).toBe("2 [3,4]")
    confirmDialog("shareLimit", 2, 60, -1)
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[0, expect.objectContaining({ action: "setShareLimit", ratioLimit: 2, seedingTimeLimit: 60, inactiveSeedingTimeLimit: -1, targets: [{ instanceId: 3, hash: "a" }, { instanceId: 4, hash: "b" }] })]])
    expect(dialogTitle()).toBeNull()
  })

  it("sends the two speed limits in two requests, and closes when both succeed", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setSpeedLimits" }))
    confirmDialog("speedLimits", 100, 200)
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([
      [INSTANCE_ID, expect.objectContaining({ action: "setUploadLimit", uploadLimit: 100, hashes: ["h1"] })],
      [INSTANCE_ID, expect.objectContaining({ action: "setDownloadLimit", downloadLimit: 200, hashes: ["h1"] })],
    ])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setUploadLimit/1")
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setDownloadLimit/1")
    expect(dialogTitle()).toBeNull()
  })

  it("sends only the upload limit when the download limit is negative", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setSpeedLimits" }))
    confirmDialog("speedLimits", 500, -1)
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "setUploadLimit", uploadLimit: 500 })]])
    expect(dialogTitle()).toBeNull()
  })

  // regression: #3992
  it("stays pending until both speed-limit requests settle", async () => {
    let finishUpload: () => void = () => {}
    mockedApi.bulkAction
      .mockImplementationOnce(() => new Promise(resolve => { finishUpload = () => resolve(undefined as never) }))
      .mockResolvedValueOnce(undefined as never)
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setSpeedLimits" }))
    confirmDialog("speedLimits", 100, 200)
    await flush()

    expect(result.current.isPending).toBe(true)

    await act(async () => finishUpload())
    await flush()

    expect(result.current.isPending).toBe(false)
    expect(dialogTitle()).toBeNull()
  })

  it("leaves the speed limits open when one request fails", async () => {
    mockedApi.bulkAction.mockResolvedValueOnce(undefined as never).mockRejectedValueOnce(new Error("qbit down"))
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "setSpeedLimits" }))
    confirmDialog("speedLimits", 100, 200)
    await flush()

    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.failed.setDownloadLimit/1", { description: "qbit down" })
    expect(dialogTitle()).toBe("speedLimits")
  })

  it("shows the comment of one torrent from its instance, and sends the comment", async () => {
    const torrent = { ...makeTorrent({ hash: "a" }), instanceId: 3 }
    const { result } = renderModule({ instanceId: ALL_INSTANCES_ID, actionTarget: pairs([{ instanceId: 3, hash: "a" }], [torrent]) })

    act(() => result.current.pick({ action: "setComment" }))
    expect(dialogValues("comment")).toBe("1 3 \"a\"")
    confirmDialog("comment", "note")
    await flush()

    expect(mockedApi.bulkAction.mock.calls).toEqual([[0, expect.objectContaining({ action: "setComment", comment: "note", targets: [{ instanceId: 3, hash: "a" }] })]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.success.setComment/1")
    expect(dialogTitle()).toBeNull()
  })

  it("renames the first torrent of the Action target on the instance of the page, with the follow-up refetch", async () => {
    mockedApi.renameTorrent.mockResolvedValue(undefined)
    const actionTarget: ActionTarget = { kind: "ticks", hashes: ["a", "b"], count: 2, previewTorrents: [makeTorrent({ hash: "a", name: "Old.Name" }), makeTorrent({ hash: "b" })] }
    const { result, refetchSpy } = renderModule({ actionTarget })

    act(() => result.current.pick({ action: "renameTorrent" }))
    expect(dialogValues("rename")).toBe("\"Old.Name\"")
    confirmDialog("rename", "  New.Name ")
    await flush()

    expect(mockedApi.renameTorrent.mock.calls).toEqual([[INSTANCE_ID, "a", "New.Name"]])
    expect(mockedToast.success).toHaveBeenCalledWith("actionToasts.renameTorrentSuccess/New.Name")
    expect(dialogTitle()).toBeNull()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(750)
    })
    expect(countListRefetches(refetchSpy)).toBe(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000)
    })
    expect(countListRefetches(refetchSpy)).toBe(2)
  })

  it("rejects an empty name with an error toast and no request", async () => {
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "renameTorrent" }))
    confirmDialog("rename", "   ")
    await flush()

    expect(mockedApi.renameTorrent).not.toHaveBeenCalled()
    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.renameTorrentEmpty")
    expect(dialogTitle()).toBe("rename")
  })

  it("leaves the rename open when the request fails", async () => {
    mockedApi.renameTorrent.mockRejectedValueOnce(new Error("taken"))
    const { result } = renderModule({ actionTarget: ticks("h1") })

    act(() => result.current.pick({ action: "renameTorrent" }))
    confirmDialog("rename", "new")
    await flush()

    expect(mockedToast.error).toHaveBeenCalledWith("actionToasts.renameTorrentFailed")
    expect(dialogTitle()).toBe("rename")
  })
})

describe("bulk action module - export", () => {
  it("exports the Select all scope in the sort of the list, with its own busy state", async () => {
    let finish = () => {}
    mockedApi.getTorrents.mockReturnValueOnce(new Promise(resolve => {
      finish = () => resolve({ torrents: [], hasMore: false } as never)
    }))
    const { result } = renderModule({ actionTarget: selectAll(40, { search: "linux" }), listFacts: { ...LIST_FACTS, sortField: "size", sortOrder: "asc" } })

    act(() => result.current.pick({ action: "export" }))
    await flush()
    expect(mockedApi.getTorrents).toHaveBeenCalledWith(INSTANCE_ID, expect.objectContaining({ search: "linux", sort: "size", order: "asc" }))
    expect(result.current.isExporting).toBe(true)
    expect(result.current.isPending).toBe(false)

    act(() => result.current.pick({ action: "pause" }))
    await flush()
    expect(mockedApi.bulkAction.mock.calls).toEqual([[INSTANCE_ID, expect.objectContaining({ action: "pause", selectAll: true })]])

    finish()
    await flush()
    expect(result.current.isExporting).toBe(false)
    expect(mockedToast.info).toHaveBeenCalledWith("contextMenu.toast.noTorrentsFoundToExport")
  })
})

describe("bulk action module - renders", () => {
  // The desktop table reads the module. A dialog that opens or closes must not render the table again.
  it("keeps the value of an Action surface when a dialog opens and closes", () => {
    const { wrapper } = makeHarness()
    let surfaceRenders = 0
    const picks: Array<ReturnType<typeof useBulkActions>["pick"]> = []
    function Surface() {
      surfaceRenders++
      picks.push(useBulkActions().pick)
      return null
    }
    function Layout() {
      const { publishSummary } = useTorrentSelectionContext()
      return <button onClick={() => publishSummary({ instanceId: INSTANCE_ID, totalSize: 0, excludedSize: 0, listFacts: LIST_FACTS, actionTarget: ticks("h1", "h2") })}>publish</button>
    }
    render(<><Layout /><Surface /></>, { wrapper })
    clickButton("publish")
    const rendersBefore = surfaceRenders

    act(() => picks[0]({ action: "recheck" }))
    expect(dialogTitle()).toBe("recheckDialog.title/2")
    clickButton("recheckDialog.cancel")
    expect(dialogTitle()).toBeNull()

    expect(surfaceRenders).toBe(rendersBefore)
    expect(new Set(picks).size).toBe(1)
  })
})
