/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Whole-table mount smoke test — the cross-PR regression tripwire for the
 * TorrentTableOptimized decomposition (issue #1963).
 *
 * It mounts the REAL component with the network/router/stream boundaries
 * mocked and a deterministic virtualizer, then asserts the behaviours most
 * likely to break while extracting seams: rows render, the header
 * select-all toggles row selection, and cross-seed mode keeps the rows the
 * backend matched for a multi-word search (#2525). The external data/action
 * hooks mocked here are NOT what the decomposition refactors, so this harness
 * stays stable across the 10 PRs it guards; `scenario` is the only per-test
 * knob they read. Real virtualized scrolling and column-filter
 * UI are intentionally left to per-seam unit tests + the manual smoke per PR.
 *
 * IMPORTANT: every mocked hook returns a STABLE singleton reference. Returning
 * fresh objects per render makes the table's effects re-fire forever (OOM).
 */

import { TorrentTableOptimized } from "@/components/torrents/TorrentTableOptimized"
import { TooltipProvider } from "@/components/ui/tooltip"
import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { api } from "@/lib/api"
import { columnFiltersToExpr } from "@/lib/column-filter-utils"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, renderHook, waitFor, within } from "@testing-library/react"
import type { ComponentProps, ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { makeTorrent } from "@/test/mockTorrent"
import { makeFilters } from "@/test/mockFilters"
import type { ActionTarget } from "@/lib/torrent-selection"
import { formatBytes } from "@/lib/utils"
import type { LocalCrossSeedMatch, Torrent } from "@/types"

// Per-test knobs read by the hoisted mocks below. Reset in beforeEach.
const scenario = vi.hoisted(() => ({
  routeSearch: "",
  isCrossSeedFiltering: false,
  totalCount: 3,
  // The loaded rows; undefined means `torrents`.
  rows: undefined as Torrent[] | undefined,
  renameDialogOpen: false,
  deleteDialogOpen: false,
  totalSize: 0,
  copyGroupsReady: undefined as boolean | undefined,
  // The options that the table last gave the list hook.
  listOptions: undefined as { groupCopies?: boolean } | undefined,
  isDemo: false,
}))
// The real list controller, so a test can read its load-more window.
const controller = vi.hoisted(() => ({ current: undefined as undefined | { loadedRows: number; reportNearEnd: () => void } }))

const torrents = [
  makeTorrent({ hash: "hash-aaa", name: "Alpha Release", state: "downloading", progress: 0.5 }),
  makeTorrent({ hash: "hash-bbb", name: "Bravo Release", state: "uploading", progress: 1 }),
  makeTorrent({ hash: "hash-ccc", name: "Charlie Release", state: "stalledUP", progress: 1 }),
]

// i18n: keep the real exports (i18n bootstrap needs initReactI18next), but
// override useTranslation with a stable passthrough translator.
vi.mock("react-i18next", async (importOriginal) => {
  const translation = {
    // Copy group keys echo their values, so a test can assert the count.
    t: (key: string | string[], options?: Record<string, unknown>) =>
      typeof key === "string" && key.startsWith("copyGroups.") && options ? `${key} ${JSON.stringify(options)}` : key,
    i18n: { resolvedLanguage: "en", language: "en" },
  }
  return {
    ...(await importOriginal<typeof import("react-i18next")>()),
    useTranslation: () => translation,
  }
})

// Router: avoid needing a real router tree.
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const search = { get q() { return scenario.routeSearch || undefined } }
  const navigate = vi.fn()
  return {
    ...(await importOriginal<typeof import("@tanstack/react-router")>()),
    useSearch: () => search,
    useNavigate: () => navigate,
  }
})

// SyncStream: keep the pure error-code helper, stub the live connection.
vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => {
  const streamState = {
    connected: false,
    error: null,
    lastMeta: undefined,
    retrying: false,
    nextRetryAt: undefined,
    retryAttempt: 0,
  }
  return {
    ...(await importOriginal<typeof import("@/contexts/SyncStreamContext")>()),
    useSyncStream: () => streamState,
  }
})

// Deterministic virtualizer: render all `count` rows regardless of jsdom
// layout. One stable singleton; the items change only with the count.
vi.mock("@tanstack/react-virtual", () => {
  let count = 0
  let items: { index: number; key: number; start: number; size: number }[] = []
  const virtualizer = {
    getVirtualItems: () => items,
    getTotalSize: () => count * 40,
    measure: vi.fn(),
    scrollToOffset: vi.fn(),
    scrollToIndex: vi.fn(),
  }
  return {
    useVirtualizer: (options: { count: number }) => {
      if (options.count !== count) {
        count = options.count
        items = Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 40, size: 40 }))
      }
      return virtualizer
    },
  }
})

vi.mock("@/hooks/useTorrentListController", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks/useTorrentListController")>()
  return {
    ...actual,
    useTorrentListController: (params: Parameters<typeof actual.useTorrentListController>[0]) => {
      const result = actual.useTorrentListController(params)
      controller.current = result
      return result
    },
  }
})

vi.mock("@/lib/demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/demo")>()),
  get isDemo() { return scenario.isDemo },
}))

// The grid measures its width through ResizeObserver, which jsdom lacks.
vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

// Network boundary: any api.* call resolves to undefined (real hooks degrade
// gracefully via their ?? fallbacks). One spy per method so a test can read
// what the table sent.
vi.mock("@/lib/api", () => ({
  api: new Proxy({} as Record<string, ReturnType<typeof vi.fn>>, {
    get: (target, prop: string) => (target[prop] ??= vi.fn(() => Promise.resolve(undefined))),
  }),
}))

// Tracker query hooks subscribe to the activity stream (needs a provider);
// the table only reads their `.data`, so stub that directly with stable refs.
vi.mock("@/hooks/useTrackerIcons", () => {
  const result = { data: {} as Record<string, string> }
  return { useTrackerIcons: () => result }
})
vi.mock("@/hooks/useTrackerCustomizations", () => {
  const result = { data: [] as unknown[] }
  return { useTrackerCustomizations: () => result }
})

// The data hook that drives the table body. Built lazily (reads `torrents`
// after init) and cached so the reference is stable across renders.
vi.mock("@/hooks/useTorrentsList", () => {
  let result: Record<string, unknown> | undefined
  return {
    TORRENT_STREAM_POLL_INTERVAL_SECONDS: 2,
    useTorrentsList: (_instanceId: number, options: { groupCopies?: boolean }) => {
      scenario.listOptions = options
      result ??= {
        get torrents() { return scenario.rows ?? torrents },
        get totalCount() { return scenario.totalCount },
        stats: {
          total: torrents.length,
          downloading: 1,
          seeding: 2,
          paused: 0,
          error: 0,
          totalDownloadSpeed: 0,
          totalUploadSpeed: 0,
          totalDownloadData: 0,
          totalUploadData: 0,
          get totalSize() { return scenario.totalSize },
        },
        counts: { status: {}, categories: {}, tags: {}, trackers: {} },
        categories: {},
        tags: [] as string[],
        trackerHealthSupported: false,
        serverState: null,
        capabilities: undefined,
        useSubcategories: false,
        isLoading: false,
        isCachedData: false,
        isStaleData: false,
        isLoadingMore: false,
        hasLoadedAll: true,
        loadMore: vi.fn(),
        streamConnected: false,
        streamMeta: undefined,
        isStreaming: false,
        streamError: null,
        streamRetrying: false,
        streamNextRetryAt: undefined,
        streamRetryAttempt: 0,
        get isCrossSeedFiltering() { return scenario.isCrossSeedFiltering },
        get copyGroupsReady() { return scenario.copyGroupsReady },
        isCrossInstanceEndpoint: false,
      }
      return result
    },
  }
})

// The action hook that drives dialog state (all dialogs closed). Every handler
// is the same spy, so a test can read the Action target that an action got.
const actionHandler = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useTorrentActions", () => {
  const noop = actionHandler
  const actions = new Proxy(
    {
      blockCrossSeeds: false,
      deleteCrossSeeds: false,
      deleteFiles: false,
      isDeleteFilesLocked: false,
      pendingTmmEnable: false,
      isPending: false,
    } as Record<string, unknown>,
    {
      // Unknown dialog flags read as falsy; unknown handlers read as no-ops.
      get(target, prop: string) {
        if (prop in target) return target[prop]
        if (prop === "showRenameTorrentDialog") return scenario.renameDialogOpen
        if (prop === "showDeleteDialog") return scenario.deleteDialogOpen
        if (prop.startsWith("show")) return false
        return noop
      },
    }
  )
  return {
    TORRENT_ACTIONS: { DELETE: "delete" },
    useTorrentActions: () => actions,
  }
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  // jsdom has no execCommand; the copy tests below stub one in.
  delete (document as { execCommand?: unknown }).execCommand
})

function isRowChecked(container: HTMLElement, name: string) {
  const row = within(container).getByText(name).closest(".cursor-pointer")
  return row?.querySelector("[role=\"checkbox\"]")?.getAttribute("aria-checked") === "true"
}

function checkedRows(container: HTMLElement) {
  return torrents.map(t => t.name).filter(name => isRowChecked(container, name))
}

// Opens the menu of the row and clicks Pause. Returns the ticked hashes of the Action target, undefined in Select all mode.
async function pauseFromMenu(container: HTMLElement, name: string, findByRole: (role: string, options: { name: RegExp }) => Promise<HTMLElement>) {
  fireEvent.contextMenu(within(container).getByText(name))
  fireEvent.click(await findByRole("menuitem", { name: /^contextMenu\.pause/ }))
  await waitFor(() => expect(actionHandler).toHaveBeenCalled())
  return (actionHandler.mock.calls[0][1] as { hashes?: string[] }).hashes
}

function renderTable(props: Partial<ComponentProps<typeof TorrentTableOptimized>> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <TorrentSelectionProvider>
        <TooltipProvider>{children}</TooltipProvider>
      </TorrentSelectionProvider>
    </QueryClientProvider>
  )
  return render(<TorrentTableOptimized instanceId={1} filters={makeFilters()} {...props} />, { wrapper })
}

describe("TorrentTableOptimized smoke", () => {
  beforeEach(() => {
    localStorage.clear()
    scenario.routeSearch = ""
    scenario.isCrossSeedFiltering = false
    scenario.totalCount = torrents.length
    scenario.rows = undefined
    scenario.renameDialogOpen = false
  })

  it("renders a row for each torrent", () => {
    const { container } = renderTable()
    expect(container.textContent).toContain("Alpha Release")
    expect(container.textContent).toContain("Bravo Release")
    expect(container.textContent).toContain("Charlie Release")
  })

  it("toggles row selection via the header select-all checkbox", () => {
    const { container } = renderTable()
    const checkboxes = container.querySelectorAll("[role=\"checkbox\"]")
    expect(checkboxes.length).toBeGreaterThan(0)
    const header = checkboxes[0]
    expect(header.getAttribute("aria-checked")).not.toBe("true")
    fireEvent.click(header)
    // Exclude the header itself so this asserts ROW checkboxes became checked,
    // not just the select-all control reflecting its own click.
    const checkedRowsAfter = within(container).getAllByRole("checkbox").filter(
      (cb) => cb !== header && cb.getAttribute("aria-checked") === "true"
    )
    expect(checkedRowsAfter.length).toBeGreaterThan(0)
  })

  it("clears column filters without clearing sidebar filters or sorting", () => {
    localStorage.setItem("qui-column-filters-1", JSON.stringify([{ columnId: "ratio", operation: "lt", value: "1" }]))
    localStorage.setItem("qui-column-sorting:1", JSON.stringify([{ id: "added_on", desc: true }]))
    const columns = renderHook(() => usePersistedColumnFilters(1))
    const sorting = renderHook(() => usePersistedColumnSorting([], 1))
    const onFilterChange = vi.fn()
    const { getByRole } = renderTable({ filters: makeFilters({ categories: ["Movies"] }), onFilterChange })

    fireEvent.click(getByRole("button", { name: "tableView.clearAllColumnFilters" }))

    expect(columns.result.current[0]).toEqual([])
    expect(sorting.result.current[0]).toEqual([{ id: "added_on", desc: true }])
    expect(onFilterChange).not.toHaveBeenCalled()
  })

  // Issue #3364: in select all mode a plain click selects only the clicked row,
  // so the next menu action reaches that row and not every matching torrent.
  it("plain click in select all mode selects only the clicked row", async () => {
    const { container, findByRole } = renderTable()
    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)
    expect(checkedRows(container)).toEqual(["Alpha Release", "Bravo Release", "Charlie Release"])

    fireEvent.click(within(container).getByText("Bravo Release"))

    expect(checkedRows(container)).toEqual(["Bravo Release"])
    expect(await pauseFromMenu(container, "Bravo Release", findByRole)).toEqual(["hash-bbb"])
  })

  it("plain click on a row in a multi-selection selects only that row", () => {
    const { container } = renderTable()
    fireEvent.click(within(container).getByText("Alpha Release"))
    fireEvent.click(within(container).getByText("Bravo Release"), { ctrlKey: true })
    expect(checkedRows(container)).toEqual(["Alpha Release", "Bravo Release"])

    fireEvent.click(within(container).getByText("Alpha Release"))

    expect(checkedRows(container)).toEqual(["Alpha Release"])
  })

  // Issue #3364: a right-click on a row excluded from select all must not act
  // on the rest of the selection.
  it("right-click on a row outside the selection selects only that row", async () => {
    const { container, findByRole } = renderTable()
    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)
    fireEvent.click(within(container).getByText("Alpha Release"), { ctrlKey: true })
    expect(checkedRows(container)).toEqual(["Bravo Release", "Charlie Release"])

    expect(await pauseFromMenu(container, "Alpha Release", findByRole)).toEqual(["hash-aaa"])
    expect(checkedRows(container)).toEqual(["Alpha Release"])
  })

  it("shift-click after a right-click on a row outside the selection ranges from the right-clicked row", () => {
    const { container } = renderTable()
    fireEvent.click(within(container).getByText("Alpha Release"))
    fireEvent.contextMenu(within(container).getByText("Charlie Release"))

    fireEvent.click(within(container).getByText("Bravo Release"), { shiftKey: true })

    expect(checkedRows(container)).toEqual(["Bravo Release", "Charlie Release"])
  })

  it("click on the open row closes it when select all reaches only that row", () => {
    const onTorrentSelect = vi.fn()
    const { container } = renderTable({ selectedTorrent: torrents[1], onTorrentSelect })
    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)
    fireEvent.click(within(container).getByText("Alpha Release"), { ctrlKey: true })
    fireEvent.click(within(container).getByText("Charlie Release"), { ctrlKey: true })
    expect(checkedRows(container)).toEqual(["Bravo Release"])

    fireEvent.click(within(container).getByText("Bravo Release"))

    expect(onTorrentSelect).toHaveBeenLastCalledWith(null)
    expect(checkedRows(container)).toEqual([])
  })

  it("click on the open row keeps the panel open until the row is the only selection", () => {
    const onTorrentSelect = vi.fn()
    const { container } = renderTable({ selectedTorrent: torrents[1], onTorrentSelect })
    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)

    fireEvent.click(within(container).getByText("Bravo Release"))
    // The page closes the panel when it gets the open torrent again.
    expect(onTorrentSelect).not.toHaveBeenCalled()
    expect(checkedRows(container)).toEqual(["Bravo Release"])

    fireEvent.click(within(container).getByText("Bravo Release"))
    expect(onTorrentSelect).toHaveBeenLastCalledWith(null)
    expect(checkedRows(container)).toEqual([])
  })

  // Issue #2525: in cross-seed mode the table filters client-side. The search
  // already narrowed rows on the backend (every word must appear somewhere in
  // the name), so the table must not re-apply it as a literal substring.
  it("keeps backend-matched rows when a multi-word search meets the cross-seed filter", () => {
    scenario.routeSearch = "release a"
    scenario.isCrossSeedFiltering = true
    const { container } = renderTable()
    expect(container.textContent).toContain("Alpha Release")
    expect(container.textContent).toContain("Bravo Release")
  })

  // Issue #1925 on the copy side: when select-all reaches past the loaded rows,
  // the field request joins the column filter with the filter expression like a
  // bulk action does, not the list expression.
  it("copy-all names past the loaded rows sends the column filter AND the filter expression", async () => {
    const expr = "Ratio > 1"
    const columnFilter = { columnId: "name", operation: "contains", value: "Release" } as const
    localStorage.setItem("qui-column-filters-1", JSON.stringify([columnFilter]))
    scenario.totalCount = 10
    vi.mocked(api.getTorrentField).mockResolvedValue({ values: ["Alpha Release"], total: 1 })
    // jsdom has no clipboard; the copy fallback calls execCommand.
    document.execCommand = vi.fn(() => true)
    const { container, findByRole } = renderTable({ filters: makeFilters({ expr }) })

    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)
    fireEvent.contextMenu(within(container).getByText("Alpha Release"))
    fireEvent.keyDown(await findByRole("menuitem", { name: "contextMenu.copy" }), { key: "ArrowRight" })
    fireEvent.click(await findByRole("menuitem", { name: "contextMenu.copyName" }))

    await waitFor(() => expect(api.getTorrentField).toHaveBeenCalled())
    const [, field, request] = vi.mocked(api.getTorrentField).mock.calls[0]
    expect(field).toBe("name")
    expect(request.filters?.expr).toBe(`(${columnFiltersToExpr([columnFilter])}) && (${expr})`)
  })

  // Issue #1925 on the copy side: in cross-seed mode the column filter applies
  // client-side, so select-all must mean the rows the user sees. Copy-all names
  // then copies those rows only, and the selection count says so.
  it("copy-all names in cross-seed mode copies the rows the column filter left visible", async () => {
    localStorage.setItem("qui-column-filters-1", JSON.stringify([{ columnId: "name", operation: "contains", value: "Alpha" }]))
    scenario.isCrossSeedFiltering = true
    let copied = ""
    // jsdom has no clipboard; the copy fallback selects a textarea and calls execCommand.
    document.execCommand = vi.fn(() => {
      copied = (document.activeElement as HTMLTextAreaElement).value
      return true
    })
    const { container, findByRole } = renderTable({ filters: makeFilters({ expr: "Hash == \"hash-aaa\" || Hash == \"hash-bbb\"" }) })
    expect(container.textContent).not.toContain("Bravo Release")

    fireEvent.click(container.querySelector("[role=\"checkbox\"]") as Element)
    fireEvent.contextMenu(within(container).getByText("Alpha Release"))
    // The copy items sit in a submenu; ArrowRight opens it without pointer events.
    fireEvent.keyDown(await findByRole("menuitem", { name: "contextMenu.copy" }), { key: "ArrowRight" })
    fireEvent.click(await findByRole("menuitem", { name: "contextMenu.copyName" }))

    await waitFor(() => expect(copied).toBe("Alpha Release"))
    expect(api.getTorrentField).not.toHaveBeenCalled()
  })
})

// A release on three trackers: one Copy group with two hidden copies, and one
// plain row. Server order under the default sort (added_on desc): the group
// takes the place of its first-sorted member, the Group representative is on
// top, then the other members in sort order.
function copyGroupRows(overrides: { top?: Partial<Torrent>; sorted?: Partial<Torrent>; third?: Partial<Torrent> } = {}) {
  return [
    makeTorrent({ hash: "hash-top", name: "Delta Release", added_on: 1_700_000_000, priority: 0, copy_group: { id: "g1", first_sorted: { instance_id: 1, hash: "hash-sorted" } }, ...overrides.top }),
    makeTorrent({ hash: "hash-sorted", name: "Delta Release copy B", added_on: 1_750_000_000, copy_group: { id: "g1" }, ...overrides.sorted }),
    makeTorrent({ hash: "hash-third", name: "Delta Release copy C", added_on: 1_710_000_000, copy_group: { id: "g1" }, ...overrides.third }),
    makeTorrent({ hash: "hash-plain", name: "Echo Release", added_on: 1_720_000_000 }),
  ]
}

const pills = (container: HTMLElement) => Array.from(container.querySelectorAll<HTMLButtonElement>("button[aria-label^=\"copyGroups.\"]"))
const warnings = (container: HTMLElement) => container.querySelectorAll("button[aria-label^=\"copyGroups.copiesWithProblem\"]")
const rowOf = (container: HTMLElement, name: string) => within(container).getByText(name).closest(".cursor-pointer") as HTMLElement
const cellOf = (row: HTMLElement, columnId: string) => row.querySelector(`[data-torrent-column-measure="${columnId}"]`) as HTMLElement

const GROUP_COPIES_KEY = "qui-group-copies"

// The columns menu in Table and Dense, the sort menu in Stacked. The menu stays open after a press of its items.
async function listMenuItem(view: ReturnType<typeof render>, role: "menuitem" | "menuitemcheckbox", name: string | RegExp) {
  if (!view.queryByRole("menu")) {
    const trigger = localStorage.getItem("qui-torrent-desktop-view-mode") === "compact"
      ? view.container.querySelector<HTMLElement>("[aria-haspopup='menu']")
      : view.getByRole("button", { name: "tableView.toggleColumns" })
    if (!trigger) throw new Error("no list menu")
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false })
  }
  return view.findByRole(role, { name })
}

async function pressExpandAll(view: ReturnType<typeof render>) {
  fireEvent.click(await listMenuItem(view, "menuitem", /^copyGroups\.(expandAll|collapseAll)$/))
}

function renderGroups(rows: Torrent[], props: Partial<ComponentProps<typeof TorrentTableOptimized>> = {}) {
  localStorage.setItem(GROUP_COPIES_KEY, "true")
  scenario.rows = rows
  scenario.totalCount = rows.length
  scenario.copyGroupsReady = true
  const view = renderTable(props)
  return {
    ...view,
    // A new prop renders the memoized table again with the new rows.
    showRows(next: Torrent[]) {
      scenario.rows = next
      view.rerender(<TorrentTableOptimized instanceId={1} filters={makeFilters()} {...props} onFilteredDataUpdate={vi.fn()} />)
    },
  }
}

describe("TorrentTableOptimized Copy groups", () => {
  beforeEach(() => {
    localStorage.clear()
    scenario.routeSearch = ""
    scenario.isCrossSeedFiltering = false
    scenario.rows = undefined
    scenario.renameDialogOpen = false
    scenario.deleteDialogOpen = false
    scenario.totalSize = 0
    scenario.copyGroupsReady = undefined
  })

  it("shows no pill on rows with no group id", () => {
    scenario.rows = [...torrents.slice(0, 2), makeTorrent({ hash: "hash-err", name: "Error Release", state: "error" })]
    const { container } = renderTable()

    expect(pills(container)).toEqual([])
    expect(warnings(container).length).toBe(0)
    expect(container.textContent).not.toContain("copyGroups.badge")
  })

  it("gives the grid an unknown row count on a grouped list and the torrent total on a flat list", () => {
    scenario.totalCount = 40
    const flat = renderTable()
    expect(flat.container.querySelector("[role=\"grid\"]")?.getAttribute("aria-rowcount")).toBe("40")
    flat.unmount()

    const grouped = renderGroups(copyGroupRows())
    expect(grouped.container.querySelector("[role=\"grid\"]")?.getAttribute("aria-rowcount")).toBe("-1")
  })

  describe.each(["normal", "dense", "compact"] as const)("in the %s row style", (viewMode) => {
    beforeEach(() => {
      localStorage.setItem("qui-torrent-desktop-view-mode", viewMode)
    })

    it("shows a collapsed group as one row with a pill that counts the hidden copies", () => {
      const { container } = renderGroups(copyGroupRows())

      expect(container.textContent).toContain("Delta Release")
      expect(container.textContent).toContain("Echo Release")
      expect(container.textContent).not.toContain("Delta Release copy C")
      const [pill, ...others] = pills(container)
      expect(others).toEqual([])
      expect(pill.getAttribute("aria-expanded")).toBe("false")
      expect(pill.getAttribute("aria-label")).toBe("copyGroups.copies {\"count\":2,\"name\":\"Delta Release\"}")
      expect(pill.textContent).toContain("copyGroups.badge {\"hidden\":2}")
      expect(rowOf(container, "Delta Release").contains(pill)).toBe(true)
    })

    it("opens and closes the group from the pill without a change to the ticks or the details panel", () => {
      const onTorrentSelect = vi.fn()
      const { container } = renderGroups(copyGroupRows(), { onTorrentSelect })
      fireEvent.click(within(container).getByText("Echo Release"), { ctrlKey: true })
      expect(isRowChecked(container, "Echo Release")).toBe(true)

      fireEvent.click(pills(container)[0])

      expect(container.textContent).toContain("Delta Release copy B")
      expect(container.textContent).toContain("Delta Release copy C")
      expect(pills(container).map(c => c.getAttribute("aria-expanded"))).toEqual(["true"])
      expect(isRowChecked(container, "Delta Release")).toBe(false)
      expect(isRowChecked(container, "Echo Release")).toBe(true)

      fireEvent.click(pills(container)[0])

      expect(container.textContent).not.toContain("Delta Release copy C")
      expect(isRowChecked(container, "Echo Release")).toBe(true)
      expect(onTorrentSelect).not.toHaveBeenCalled()
    })

    it("opens and closes all groups from Expand all in the menu, with one pill while they are open", async () => {
      const view = renderGroups(copyGroupRows())

      await pressExpandAll(view)
      expect(view.container.textContent).toContain("Delta Release copy C")
      expect(pills(view.container).length).toBe(1)
      expect((await listMenuItem(view, "menuitem", "copyGroups.collapseAll"))).toBeTruthy()

      await pressExpandAll(view)
      expect(view.container.textContent).not.toContain("Delta Release copy C")
    })

    it("shows the warning pill when a hidden copy changes to an error, and a plain pill when the group is open", () => {
      const rows = copyGroupRows()
      const view = renderGroups(rows)
      expect(warnings(view.container).length).toBe(0)

      // The top row keeps its object: only the hidden copy changes.
      view.showRows([rows[0], rows[1], { ...rows[2], state: "error" }, rows[3]])
      expect(warnings(view.container).length).toBe(1)

      fireEvent.click(pills(view.container)[0])
      expect(warnings(view.container).length).toBe(0)
    })

    it("shows the name of the first-sorted member under a name sort, and the menu and the rename dialog get the Group representative", async () => {
      localStorage.setItem("qui-column-sorting:1", JSON.stringify([{ id: "name", desc: false }]))
      const rows = copyGroupRows({ sorted: { name: "Alpha Copy Of Delta" } })
      const onTorrentSelect = vi.fn()
      const view = renderGroups(rows, { onTorrentSelect })

      expect(view.container.textContent).toContain("Alpha Copy Of Delta")
      expect(view.container.textContent).not.toContain("Delta Release")

      fireEvent.contextMenu(within(view.container).getByText("Alpha Copy Of Delta"))
      fireEvent.click(await view.findByRole("menuitem", { name: "contextMenu.viewDetails" }))
      expect(onTorrentSelect).toHaveBeenLastCalledWith(rows[0])

      fireEvent.contextMenu(within(view.container).getByText("Alpha Copy Of Delta"))
      scenario.renameDialogOpen = true
      view.showRows(rows)

      const input = await view.findByDisplayValue("Delta Release")
      expect((input as HTMLInputElement).value).toBe("Delta Release")
    })
  })

  describe.each(["normal", "dense"] as const)("in the %s row style", (viewMode) => {
    beforeEach(() => {
      localStorage.setItem("qui-torrent-desktop-view-mode", viewMode)
    })

    it("shows the Added value of the first-sorted member on a collapsed row with no sort set by the user", () => {
      const { container } = renderGroups(copyGroupRows())
      const collapsedAdded = cellOf(rowOf(container, "Delta Release"), "added_on").textContent

      fireEvent.click(pills(container)[0])

      // An open group shows the own value of each row.
      expect(collapsedAdded).toBe(cellOf(rowOf(container, "Delta Release copy B"), "added_on").textContent)
      expect(collapsedAdded).not.toBe(cellOf(rowOf(container, "Delta Release"), "added_on").textContent)
    })

    it("updates the sorted value of a collapsed row when its hidden first-sorted copy changes", () => {
      const rows = copyGroupRows()
      const view = renderGroups(rows)
      const before = cellOf(rowOf(view.container, "Delta Release"), "added_on").textContent

      // The top row keeps its object: only the hidden copy changes.
      view.showRows([rows[0], { ...rows[1], added_on: 1_760_000_000 }, rows[2], rows[3]])

      expect(cellOf(rowOf(view.container, "Delta Release"), "added_on").textContent).not.toBe(before)
    })

    it("puts a working pill next to the checkbox when the Name column is hidden", () => {
      localStorage.setItem("qui-column-visibility:1", JSON.stringify({ name: false }))
      const { container } = renderGroups(copyGroupRows())
      const selectCells = () => container.querySelectorAll("[data-torrent-column-measure=\"select\"]")
      const collapsedRows = selectCells().length

      const pill = selectCells()[0].querySelector("button[aria-label^=\"copyGroups.\"]") as HTMLButtonElement
      expect(pill).not.toBeNull()
      fireEvent.click(pill)

      expect(selectCells().length).toBe(collapsedRows + 2)
    })
  })

  it("keeps the Stacked row on the representative values when the sorted column is not one it shows", () => {
    localStorage.setItem("qui-torrent-desktop-view-mode", "compact")
    const { container } = renderGroups(copyGroupRows({ sorted: { name: "Alpha Copy Of Delta" } }))

    expect(container.textContent).toContain("Delta Release")
    expect(container.textContent).not.toContain("Alpha Copy Of Delta")
  })

  // regression: #3922
  it("shows the status of the first-sorted copy on a Stacked row under a sort by the status icon column", () => {
    localStorage.setItem("qui-torrent-desktop-view-mode", "compact")
    localStorage.setItem("qui-column-sorting:1", JSON.stringify([{ id: "status_icon", desc: false }]))
    // Only the hidden first-sorted copy differs between the two lists.
    const seeding = renderGroups(copyGroupRows({ sorted: { state: "uploading" } }))
    const seedingText = seeding.container.textContent
    seeding.unmount()

    const { container } = renderGroups(copyGroupRows({ sorted: { state: "pausedUP" } }))

    expect(container.textContent).not.toBe(seedingText)
  })

  describe("the tooltip of the pill", () => {
    // jsdom has no matchMedia; without one, the device counts as one that cannot hover.
    function setCanHover(canHover: boolean) {
      window.matchMedia = ((query: string) => ({
        matches: canHover && query === "(hover: hover)",
        media: query,
        addEventListener() {},
        removeEventListener() {},
      })) as unknown as typeof window.matchMedia
    }

    afterEach(() => {
      delete (window as { matchMedia?: unknown }).matchMedia
    })

    it("names the pill on a device that can hover", async () => {
      setCanHover(true)
      const { container, findByRole } = renderGroups(copyGroupRows())

      // A keyboard user tabs to the pill.
      fireEvent.keyDown(pills(container)[0], { key: "Tab" })
      act(() => pills(container)[0].focus())

      expect((await findByRole("tooltip")).textContent).toContain("copyGroups.copies {\"count\":2,\"name\":\"Delta Release\"}")
    })

    it("stays closed on a tap on a touch screen, which only opens the group", () => {
      setCanHover(false)
      const { container, queryByRole } = renderGroups(copyGroupRows())

      fireEvent.pointerDown(pills(container)[0], { pointerType: "touch" })
      fireEvent.click(pills(container)[0])

      expect(pills(container)[0].getAttribute("aria-expanded")).toBe("true")
      expect(queryByRole("tooltip")).toBeNull()
    })
  })

  // regression: #3922
  it("names the torrent that a Stacked row shows in incognito mode under a name sort", () => {
    localStorage.setItem("qui-torrent-desktop-view-mode", "compact")
    localStorage.setItem("qui-incognito-mode", "true")
    localStorage.setItem("qui-column-sorting:1", JSON.stringify([{ id: "name", desc: false }]))
    const { container } = renderGroups(copyGroupRows({ sorted: { name: "Alpha Copy Of Delta" } }))

    const pill = pills(container)[0]
    const shownName = pill.closest("div.cursor-pointer")?.querySelector("h3")?.textContent

    expect(shownName).toBeTruthy()
    expect(pill.getAttribute("aria-label")).toContain(JSON.stringify({ name: shownName }).slice(1, -1))
  })

  describe("selection", () => {
    const actionTargetOf = (call = 0) => actionHandler.mock.calls[call][1] as ActionTarget
    const headerCheckbox = (container: HTMLElement) => container.querySelector("[role=\"checkbox\"]") as HTMLElement
    const pillText = (container: HTMLElement) => pills(container)[0].textContent

    it("ticks only the collapsed row, and a bulk pause reaches only that torrent", async () => {
      const { container, findByRole } = renderGroups(copyGroupRows())
      fireEvent.click(within(container).getByText("Delta Release"), { ctrlKey: true })

      expect(await pauseFromMenu(container, "Delta Release", findByRole)).toEqual(["hash-top"])
    })

    it("acts on only the collapsed row from a right-click outside the selection", async () => {
      const { container, findByRole } = renderGroups(copyGroupRows())
      fireEvent.click(within(container).getByText("Echo Release"))

      expect(await pauseFromMenu(container, "Delta Release", findByRole)).toEqual(["hash-top"])
    })

    it("Shift-selects across a collapsed group only the visible rows", async () => {
      const rows = [makeTorrent({ hash: "hash-first", name: "Alpha Release" }), ...copyGroupRows()]
      const { container, findByRole } = renderGroups(rows)
      fireEvent.click(within(container).getByText("Alpha Release"))
      fireEvent.click(within(container).getByText("Echo Release"), { shiftKey: true })

      expect(await pauseFromMenu(container, "Echo Release", findByRole)).toEqual(["hash-first", "hash-top", "hash-plain"])
    })

    it.each([
      ["a group opens", async (view: ReturnType<typeof render>) => fireEvent.click(pills(view.container)[0])],
      ["a press of the Expand all button", pressExpandAll],
    ])("starts a Shift-select at the clicked row after %s", async (_, expand) => {
      const view = renderGroups(copyGroupRows())
      const { container } = view
      fireEvent.click(within(container).getByText("Echo Release"))

      await expand(view)
      fireEvent.click(within(container).getByText("Delta Release"), { shiftKey: true })

      expect(isRowChecked(container, "Delta Release copy B")).toBe(false)
      expect(isRowChecked(container, "Delta Release")).toBe(true)
      expect(isRowChecked(container, "Echo Release")).toBe(true)
    })

    it("checks the header checkbox when every visible row is ticked", () => {
      const { container } = renderGroups(copyGroupRows())
      fireEvent.click(within(container).getByText("Delta Release"), { ctrlKey: true })
      fireEvent.click(within(container).getByText("Echo Release"), { ctrlKey: true })

      expect(headerCheckbox(container).getAttribute("aria-checked")).toBe("true")
    })

    it("keeps a tick on a copy that its group hides, and the badge counts it", async () => {
      const { container, findByRole } = renderGroups(copyGroupRows())
      fireEvent.click(pills(container)[0])
      fireEvent.click(within(container).getByText("Delta Release copy C"), { ctrlKey: true })
      expect(pillText(container)).toContain("copyGroups.badge {\"hidden\":2}")

      fireEvent.click(pills(container)[0])

      expect(pillText(container)).toContain("copyGroups.badgeTicked {\"hidden\":2,\"count\":1}")
      expect(pills(container)[0].getAttribute("aria-label")).toContain("copyGroups.hiddenTicked")
      fireEvent.click(within(container).getByText("Echo Release"), { ctrlKey: true })
      expect(await pauseFromMenu(container, "Echo Release", findByRole)).toEqual(["hash-third", "hash-plain"])
    })

    it("counts a tick on a copy that a stream update hides", () => {
      const rows = copyGroupRows()
      const flat = rows.map(({ copy_group: _, ...row }) => row as Torrent)
      const view = renderGroups(flat)
      fireEvent.click(within(view.container).getByText("Delta Release copy B"), { ctrlKey: true })

      view.showRows(rows)

      expect(pillText(view.container)).toContain("copyGroups.badgeTicked {\"hidden\":2,\"count\":1}")
    })

    it("shows no ticks on the badge in Select all mode, and the preview holds the hidden copies", async () => {
      const { container, findByRole } = renderGroups(copyGroupRows())
      fireEvent.click(headerCheckbox(container))

      expect(pillText(container)).toContain("copyGroups.badge {\"hidden\":2}")
      expect(pills(container)[0].getAttribute("aria-label")).not.toContain("hiddenTicked")
      expect(await pauseFromMenu(container, "Echo Release", findByRole)).toBeUndefined()
      expect(actionTargetOf().previewTorrents.map(torrent => torrent.hash)).toEqual(["hash-top", "hash-sorted", "hash-third", "hash-plain"])
    })

    it("sends the same select-all request as the flat list", async () => {
      const selectAllRequest = async (rows: Torrent[]) => {
        const { container, findByRole, unmount } = renderGroups(rows)
        fireEvent.click(headerCheckbox(container))
        await pauseFromMenu(container, "Echo Release", findByRole)
        const target = actionTargetOf()
        unmount()
        actionHandler.mockClear()
        return target.kind === "selectAll" ? target.selectAll : undefined
      }
      const flat = await selectAllRequest(copyGroupRows().map(({ copy_group: _, ...row }) => row as Torrent))

      expect(flat).toBeDefined()
      expect(await selectAllRequest(copyGroupRows())).toEqual(flat)
    })

    it("counts an exclusion on a hidden copy in the select-all size", () => {
      scenario.totalSize = 4_000_000
      const rows = copyGroupRows().map(row => ({ ...row, size: 1_000_000 }))
      const { container } = renderGroups(rows)
      fireEvent.click(headerCheckbox(container))
      fireEvent.click(pills(container)[0])
      fireEvent.click(within(container).getByText("Delta Release copy C"), { ctrlKey: true })

      fireEvent.click(pills(container)[0])

      expect(container.textContent).toContain(formatBytes(3_000_000))
      expect(headerCheckbox(container).getAttribute("aria-checked")).toBe("mixed")
    })

    it("lists a hidden copy after one press of Check in the delete dialog of a collapsed row", async () => {
      vi.mocked(api.getDeleteCrossSeeds).mockResolvedValue({
        matches: [{ ...copyGroupRows()[1], instanceId: 1, instanceName: "qb", matchType: "content_path" } as unknown as LocalCrossSeedMatch],
        uncheckedInstances: [],
      })
      const view = renderGroups(copyGroupRows())
      fireEvent.click(within(view.container).getByText("Delta Release"), { ctrlKey: true })
      scenario.deleteDialogOpen = true
      view.showRows(copyGroupRows())

      fireEvent.click(await view.findByRole("button", { name: "crossSeedWarning.idle.check" }))
      fireEvent.click(await view.findByText("crossSeedWarning.showAffected"))

      expect(vi.mocked(api.getDeleteCrossSeeds).mock.calls).toEqual([[1, { targets: [{ instanceId: 1, hash: "hash-top" }] }, expect.any(AbortSignal)]])
      expect((await view.findAllByText("Delta Release copy B")).length).toBeGreaterThan(0)
    })
  })

  it("shows no warning pill for a problem on the top row itself", () => {
    const { container } = renderGroups(copyGroupRows({ top: { state: "error" } }))
    expect(warnings(container).length).toBe(0)
  })

  it.each([
    [{ state: "error" }, true],
    [{ state: "missingFiles" }, true],
    [{ tracker_health: "unregistered" }, true],
    [{ tracker_health: "tracker_down" }, true],
    [{ tracker_health: "tracker_error" }, true],
    [{ state: "stalledUP" }, false],
  ] as const)("shows the warning pill for a hidden copy with %o: %s", (hidden, expected) => {
    const { container } = renderGroups(copyGroupRows({ third: hidden }))
    expect(warnings(container).length).toBe(expected ? 1 : 0)
  })
})

describe("TorrentTableOptimized Group copies switch", () => {
  beforeEach(() => {
    localStorage.clear()
    scenario.routeSearch = ""
    scenario.isCrossSeedFiltering = false
    scenario.totalCount = torrents.length
    scenario.rows = undefined
    scenario.copyGroupsReady = undefined
    scenario.isDemo = false
  })

  afterEach(() => {
    delete (window as { matchMedia?: unknown }).matchMedia
  })

  // Table and Dense share the columns menu; Stacked has the sort menu.
  describe.each(["normal", "compact"] as const)("in the %s row style", (viewMode) => {
    beforeEach(() => {
      localStorage.setItem("qui-torrent-desktop-view-mode", viewMode)
    })

    it("asks for groups while the switch is on, with Expand all beside it", async () => {
      const view = renderTable()
      expect(scenario.listOptions?.groupCopies).toBe(false)
      const groupSwitch = await listMenuItem(view, "menuitemcheckbox", "copyGroups.groupCopies")
      expect(groupSwitch.getAttribute("aria-checked")).toBe("false")
      expect(view.queryByRole("menuitem", { name: "copyGroups.expandAll" })).toBeNull()

      fireEvent.click(groupSwitch)

      expect(scenario.listOptions?.groupCopies).toBe(true)
      expect(localStorage.getItem(GROUP_COPIES_KEY)).toBe("true")
      expect(groupSwitch.getAttribute("aria-checked")).toBe("true")
      expect(view.getByRole("menuitem", { name: "copyGroups.expandAll" })).toBeTruthy()

      fireEvent.click(groupSwitch)

      expect(scenario.listOptions?.groupCopies).toBe(false)
      expect(view.queryByRole("menuitem", { name: "copyGroups.expandAll" })).toBeNull()
    })
  })

  it("starts the loaded rows again at the first rows on a press of the switch", async () => {
    scenario.rows = Array.from({ length: 300 }, (_, i) => makeTorrent({ hash: `hash-${i}`, name: `Release ${i}` }))
    scenario.totalCount = 300
    const view = renderTable()
    act(() => controller.current?.reportNearEnd())
    expect(controller.current?.loadedRows).toBe(200)

    fireEvent.click(await listMenuItem(view, "menuitemcheckbox", "copyGroups.groupCopies"))

    expect(controller.current?.loadedRows).toBe(100)
  })

  it.each([
    [false, true],
    [undefined, false],
  ] as const)("with a ready field of %s, shows the Grouping hint: %s", (ready, shown) => {
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    scenario.copyGroupsReady = ready
    const { container } = renderTable()
    expect(container.textContent?.includes("copyGroups.grouping")).toBe(shown)
  })

  it.each([
    ["on a phone", () => {
      window.matchMedia = ((query: string) => ({
        matches: query === "(max-width: 767px)",
        media: query,
        addEventListener() {},
        removeEventListener() {},
      })) as unknown as typeof window.matchMedia
    }],
    ["in the demo", () => { scenario.isDemo = true }],
  ])("asks for no groups and shows no switch %s", async (_, setUp) => {
    setUp()
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    const view = renderTable()

    fireEvent.pointerDown(view.getByRole("button", { name: "tableView.toggleColumns" }), { button: 0, ctrlKey: false })
    await view.findByRole("menu")

    expect(scenario.listOptions?.groupCopies).toBe(false)
    expect(view.queryByRole("menuitemcheckbox", { name: "copyGroups.groupCopies" })).toBeNull()
  })
})
