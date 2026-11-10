/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: { bulkAction: vi.fn() },
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

// Keeps the count in the text, so a test can read the count of a toast or a dialog title.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => opts && "count" in opts ? `${key}/${opts.count}` : key,
  }),
}))

import { BulkActionProvider } from "../BulkActionProvider"
import { useBulkActions } from "../bulk-actions-context"
import { TorrentSelectionProvider, useTorrentSelectionContext, type TorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { api } from "@/lib/api"
import { resetPendingListRefetchesForTests } from "@/hooks/useTorrentActions"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import { EMPTY_ACTION_TARGET, type ActionTarget } from "@/lib/torrent-selection"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
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
      <TorrentSelectionProvider>
        <BulkActionProvider>{children}</BulkActionProvider>
      </TorrentSelectionProvider>
    </QueryClientProvider>
  )
  return { wrapper, refetchSpy, invalidateSpy }
}

// A Layout that publishes, and an Action surface that picks.
function renderModule(published?: Partial<TorrentSelectionSummary> & { actionTarget: ActionTarget }) {
  const harness = makeHarness()
  const { result } = renderHook(() => ({ ...useBulkActions(), publish: useTorrentSelectionContext().publishSummary }), { wrapper: harness.wrapper })
  const publish = (value: Partial<TorrentSelectionSummary> & { actionTarget: ActionTarget }) => act(() => {
    result.current.publish({ instanceId: INSTANCE_ID, selectedTotalSize: 0, ...value })
  })
  if (published) publish(published)
  return { ...harness, result, publish }
}

function ticks(...hashes: string[]): ActionTarget {
  return { kind: "ticks", hashes, count: hashes.length, previewTorrents: hashes.map(hash => makeTorrent({ hash })) }
}

function pairs(targets: TorrentActionTarget[]): ActionTarget {
  return { kind: "ticks", hashes: [], targets, count: targets.length, previewTorrents: [] }
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
  vi.useFakeTimers()
  resetPendingListRefetchesForTests()
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
      return <button onClick={() => publishSummary({ instanceId: INSTANCE_ID, selectedTotalSize: 0, actionTarget: ticks("h1", "h2") })}>publish</button>
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
