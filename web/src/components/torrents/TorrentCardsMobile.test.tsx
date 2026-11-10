/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Mounts the real cards of the mobile layout with the list hook, the network,
 * the router, and the virtualizer mocked, as the table smoke test does.
 * jsdom does no layout, so the size of the tap zone and the position of the
 * pill are left to the live proof.
 *
 * IMPORTANT: every mocked hook returns a STABLE singleton reference.
 */

import { TorrentCardsMobile } from "@/components/torrents/TorrentCardsMobile"
import { TooltipProvider } from "@/components/ui/tooltip"
import { MobileScrollProvider } from "@/contexts/MobileScrollContext"
import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
// The size and speed formatters read their unit labels from the i18n resources.
import "@/i18n"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
import { formatBytes } from "@/lib/utils"
import type { Torrent, TorrentFilters } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, within } from "@testing-library/react"
import type { ComponentProps, ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Per-test knobs read by the hoisted mocks below. Reset in beforeEach.
const scenario = vi.hoisted(() => ({
  rows: [] as Torrent[],
  totalCount: 0,
  hasLoadedAll: true,
  copyGroupsReady: undefined as boolean | undefined,
  // The options that the cards last gave the list hook.
  listOptions: undefined as { groupCopies?: boolean } | undefined,
  isDemo: false,
}))
// The real list controller, so a test can read its load-more window.
const controller = vi.hoisted(() => ({ current: undefined as undefined | { loadedRows: number; reportNearEnd: () => void } }))

vi.mock("react-i18next", async (importOriginal) => {
  const translation = {
    // Copy group, status bar, and added time keys echo their values, so a test can assert a count or a time.
    t: (key: string | string[], options?: Record<string, unknown>) =>
      typeof key === "string" && /^(copyGroups\.|statusBar\.|mobileCards\.added$)/.test(key) && options ? `${key} ${JSON.stringify(options)}` : key,
    i18n: { resolvedLanguage: "en", language: "en" },
  }
  return {
    ...(await importOriginal<typeof import("react-i18next")>()),
    useTranslation: () => translation,
  }
})

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const search = {}
  const navigate = vi.fn()
  return {
    ...(await importOriginal<typeof import("@tanstack/react-router")>()),
    useSearch: () => search,
    useNavigate: () => navigate,
  }
})

vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => {
  const streamState = { connected: false, error: null, lastMeta: undefined, retrying: false, nextRetryAt: undefined, retryAttempt: 0 }
  return {
    ...(await importOriginal<typeof import("@/contexts/SyncStreamContext")>()),
    useSyncStream: () => streamState,
  }
})

// Renders all `count` cards, because jsdom has no layout.
vi.mock("@tanstack/react-virtual", () => {
  let count = 0
  let items: { index: number; key: number; start: number; size: number }[] = []
  const virtualizer = {
    getVirtualItems: () => items,
    getTotalSize: () => count * 86,
    measure: vi.fn(),
    scrollToOffset: vi.fn(),
  }
  return {
    useVirtualizer: (options: { count: number }) => {
      if (options.count !== count) {
        count = options.count
        items = Array.from({ length: count }, (_, index) => ({ index, key: index, start: index * 86, size: 86 }))
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

vi.mock("@/lib/api", () => ({
  api: new Proxy({} as Record<string, ReturnType<typeof vi.fn>>, {
    get: (target, prop: string) => (target[prop] ??= vi.fn(() => Promise.resolve(undefined))),
  }),
}))

vi.mock("@/hooks/useTrackerIcons", () => {
  const result = { data: {} as Record<string, string> }
  return { useTrackerIcons: () => result }
})
vi.mock("@/hooks/useTrackerCustomizations", () => {
  const result = { data: [] as unknown[] }
  return { useTrackerCustomizations: () => result }
})

vi.mock("@/hooks/useTorrentsList", () => {
  let result: Record<string, unknown> | undefined
  return {
    TORRENT_STREAM_POLL_INTERVAL_SECONDS: 2,
    useTorrentsList: (_instanceId: number, options: { groupCopies?: boolean }) => {
      scenario.listOptions = options
      result ??= {
        get torrents() { return scenario.rows },
        get totalCount() { return scenario.totalCount },
        stats: { totalSize: 0 },
        counts: { status: {}, categories: {}, tags: {}, trackers: {} },
        categories: {},
        tags: [] as string[],
        trackerHealthSupported: false,
        useSubcategories: false,
        isLoading: false,
        isLoadingMore: false,
        get hasLoadedAll() { return scenario.hasLoadedAll },
        loadMore: vi.fn(),
        get copyGroupsReady() { return scenario.copyGroupsReady },
        isCrossInstanceEndpoint: false,
      }
      return result
    },
  }
})

// Every handler is the same spy, so a test can read the Action target that an action got.
const actionHandler = vi.hoisted(() => vi.fn())
vi.mock("@/hooks/useTorrentActions", () => {
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
      get(target, prop: string) {
        if (prop in target) return target[prop]
        if (prop.startsWith("show")) return false
        return actionHandler
      },
    }
  )
  return {
    TORRENT_ACTIONS: { DELETE: "delete", PAUSE: "pause", RESUME: "resume" },
    useTorrentActions: () => actions,
  }
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.useRealTimers()
})

const GROUP_COPIES_KEY = "qui-group-copies"
const VIEW_MODE_KEY = "qui-torrent-mobile-view-mode"
const SORT_KEY = "qui:torrent-mobile-sort:1"
const DATE_TIME_KEY = "qui-datetime-preferences"
const CARD_SIZES = ["normal", "compact", "ultra-compact"] as const

// The rows of one page in the order that the server sends them: the Group
// representative, then the other members in sort order, then a plain row.
function copyGroupRows(overrides: { top?: Partial<Torrent>; sorted?: Partial<Torrent>; third?: Partial<Torrent> } = {}) {
  return [
    makeTorrent({ hash: "hash-top", name: "Delta Release", copy_group: { id: "g1", first_sorted: { instance_id: 1, hash: "hash-sorted" } }, ...overrides.top }),
    makeTorrent({ hash: "hash-sorted", name: "Delta Release copy B", copy_group: { id: "g1" }, ...overrides.sorted }),
    makeTorrent({ hash: "hash-third", name: "Delta Release copy C", copy_group: { id: "g1" }, ...overrides.third }),
    makeTorrent({ hash: "hash-plain", name: "Echo Release" }),
  ]
}

function flatRows(count: number) {
  return Array.from({ length: count }, (_, i) => makeTorrent({ hash: `hash-${i}`, name: `Release ${i}` }))
}

type CardsProps = Partial<ComponentProps<typeof TorrentCardsMobile>>

function renderCards(rows: Torrent[], props: CardsProps = {}) {
  scenario.rows = rows
  scenario.totalCount ||= rows.length
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <MobileScrollProvider>
        <TorrentSelectionProvider>
          <TooltipProvider>{children}</TooltipProvider>
        </TorrentSelectionProvider>
      </MobileScrollProvider>
    </QueryClientProvider>
  )
  const filters: TorrentFilters = props.filters ?? makeFilters()
  const view = render(<TorrentCardsMobile instanceId={1} {...props} filters={filters} />, { wrapper })
  return {
    ...view,
    // New props render the cards again with the new rows.
    showRows(next: Torrent[]) {
      scenario.rows = next
      view.rerender(<TorrentCardsMobile instanceId={1} {...props} filters={filters} onFilteredDataUpdate={vi.fn()} />)
    },
  }
}

function renderGroups(rows: Torrent[] = copyGroupRows(), props: CardsProps = {}) {
  localStorage.setItem(GROUP_COPIES_KEY, "true")
  scenario.copyGroupsReady = true
  return renderCards(rows, props)
}

const cardNames = (container: HTMLElement) => Array.from(container.querySelectorAll("h3"), name => name.textContent)
const cardOf = (container: HTMLElement, name: string) => within(container).getByText(name)
const pills = (container: HTMLElement) => Array.from(container.querySelectorAll<HTMLButtonElement>("button[aria-label^=\"copyGroups.\"]"))
const warnings = (container: HTMLElement) => container.querySelectorAll("button[aria-label^=\"copyGroups.copiesWithProblem\"]")
const tickedCards = (container: HTMLElement) =>
  Array.from(container.querySelectorAll("[role=\"checkbox\"][aria-checked=\"true\"]"), box => box.closest(".cursor-pointer")?.querySelector("h3")?.textContent)

// The sort menu stays open after a press of the switch or of Expand all.
async function sortMenuItem(view: ReturnType<typeof render>, role: "menuitem" | "menuitemcheckbox", name: string | RegExp) {
  if (!view.queryByRole("menu")) {
    fireEvent.pointerDown(view.getByRole("button", { name: /^mobileCards\.sort:/ }), { button: 0, ctrlKey: false })
  }
  return view.findByRole(role, { name })
}

async function pressExpandAll(view: ReturnType<typeof render>) {
  fireEvent.click(await sortMenuItem(view, "menuitem", /^copyGroups\.(expandAll|collapseAll)$/))
}

function longPress(card: HTMLElement) {
  vi.useFakeTimers()
  fireEvent.touchStart(card, { touches: [{ clientX: 10, clientY: 10 }] })
  act(() => { vi.advanceTimersByTime(600) })
  fireEvent.touchEnd(card)
  vi.useRealTimers()
}

beforeEach(() => {
  localStorage.clear()
  scenario.rows = []
  scenario.totalCount = 0
  scenario.hasLoadedAll = true
  scenario.copyGroupsReady = undefined
  scenario.listOptions = undefined
  scenario.isDemo = false
})

describe("TorrentCardsMobile Group copies switch", () => {
  it("holds the switch in the sort menu, asks for groups while it is on, and shows Expand all only then", async () => {
    const view = renderCards(flatRows(3))
    expect(scenario.listOptions?.groupCopies).toBe(false)
    const groupSwitch = await sortMenuItem(view, "menuitemcheckbox", "copyGroups.groupCopies")
    expect(groupSwitch.getAttribute("aria-checked")).toBe("false")
    expect(view.queryByRole("menuitem", { name: "copyGroups.expandAll" })).toBeNull()

    fireEvent.click(groupSwitch)

    expect(scenario.listOptions?.groupCopies).toBe(true)
    expect(localStorage.getItem(GROUP_COPIES_KEY)).toBe("true")
    expect(view.getByRole("menuitem", { name: "copyGroups.expandAll" })).toBeTruthy()

    fireEvent.click(groupSwitch)

    expect(scenario.listOptions?.groupCopies).toBe(false)
    expect(view.queryByRole("menuitem", { name: "copyGroups.expandAll" })).toBeNull()
  })

  it("asks for no groups in Filter Cross-Seeds", () => {
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    renderCards(flatRows(3), { filters: makeFilters({ expr: "Hash == \"hash-0\" || Hash == \"hash-1\"" }) })

    expect(scenario.listOptions?.groupCopies).toBe(false)
  })

  it("asks for no groups and shows no switch in the demo", async () => {
    scenario.isDemo = true
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    const view = renderCards(flatRows(3))

    fireEvent.pointerDown(view.getByRole("button", { name: /^mobileCards\.sort:/ }), { button: 0, ctrlKey: false })
    await view.findByRole("menu")

    expect(scenario.listOptions?.groupCopies).toBe(false)
    expect(view.queryByRole("menuitemcheckbox", { name: "copyGroups.groupCopies" })).toBeNull()
  })

  it("starts the load-more window again at the first rows on a tap on the switch", async () => {
    const view = renderCards(flatRows(300))
    act(() => controller.current?.reportNearEnd())
    expect(controller.current?.loadedRows).toBe(200)

    fireEvent.click(await sortMenuItem(view, "menuitemcheckbox", "copyGroups.groupCopies"))

    expect(controller.current?.loadedRows).toBe(100)
  })
})

describe("TorrentCardsMobile Copy groups", () => {
  describe.each(CARD_SIZES)("in the %s card size", (size) => {
    beforeEach(() => {
      localStorage.setItem(VIEW_MODE_KEY, size)
    })

    it("shows a closed group as one card with a pill that counts the hidden copies, and the next torrent on the card after it", () => {
      const { container } = renderGroups()

      expect(cardNames(container)).toEqual(["Delta Release", "Echo Release"])
      const [pill, ...others] = pills(container)
      expect(others).toEqual([])
      expect(pill.getAttribute("aria-expanded")).toBe("false")
      expect(pill.getAttribute("aria-label")).toBe("copyGroups.copies {\"count\":2,\"name\":\"Delta Release\"}")
      expect(pill.textContent).toContain("copyGroups.badge {\"hidden\":2}")
      expect(cardOf(container, "Delta Release").closest(".cursor-pointer")?.contains(pill)).toBe(true)
    })

    it("opens and closes the group from the pill, with no tick and no details panel", () => {
      const onTorrentSelect = vi.fn()
      const { container } = renderGroups(copyGroupRows(), { onTorrentSelect })

      fireEvent.click(pills(container)[0])

      expect(cardNames(container)).toEqual(["Delta Release", "Delta Release copy B", "Delta Release copy C", "Echo Release"])
      expect(pills(container).map(pill => pill.getAttribute("aria-expanded"))).toEqual(["true"])

      fireEvent.click(pills(container)[0])

      expect(cardNames(container)).toEqual(["Delta Release", "Echo Release"])
      expect(container.querySelector("[role=\"checkbox\"]")).toBeNull()
      expect(onTorrentSelect).not.toHaveBeenCalled()
    })
  })

  it("shows the warning pill when a hidden copy changes to an error, and none for a problem on the top row", () => {
    const rows = copyGroupRows({ top: { state: "error" } })
    const view = renderGroups(rows)
    expect(warnings(view.container).length).toBe(0)

    // The top row keeps its object: only the hidden copy changes.
    view.showRows([rows[0], rows[1], { ...rows[2], state: "missingFiles" }, rows[3]])

    expect(warnings(view.container).length).toBe(1)
  })

  it("shows the sorted value of the first-sorted member on the card of a closed group, and a tap gives the Group representative to the details", () => {
    localStorage.setItem(SORT_KEY, JSON.stringify({ field: "name", order: "asc" }))
    const rows = copyGroupRows({ sorted: { name: "Alpha Copy Of Delta" } })
    const onTorrentSelect = vi.fn()
    const { container } = renderGroups(rows, { onTorrentSelect })

    expect(cardNames(container)).toEqual(["Alpha Copy Of Delta", "Echo Release"])

    fireEvent.click(cardOf(container, "Alpha Copy Of Delta"))

    expect(onTorrentSelect).toHaveBeenCalledExactlyOnceWith(rows[0])
  })

  describe("selection", () => {
    const pausedHashes = (view: ReturnType<typeof render>) => {
      fireEvent.click(view.getByRole("button", { name: "managementBar.pause" }))
      return (actionHandler.mock.calls[0][1] as { hashes?: string[] }).hashes
    }

    it("selects one torrent on a long press on the card of a closed group, and a bulk pause reaches only that torrent", () => {
      const view = renderGroups()

      longPress(cardOf(view.container, "Delta Release"))

      expect(tickedCards(view.container)).toEqual(["Delta Release"])
      expect(pausedHashes(view)).toEqual(["hash-top"])
    })

    it("starts no selection on a long press on the pill", () => {
      const { container } = renderGroups()

      longPress(pills(container)[0])

      expect(container.querySelector("[role=\"checkbox\"]")).toBeNull()
    })

    it("keeps a tick on a copy when its group closes, and the pill counts it", () => {
      const view = renderGroups()
      const { container } = view
      longPress(cardOf(container, "Echo Release"))
      fireEvent.click(pills(container)[0])
      fireEvent.click(cardOf(container, "Delta Release copy C"))
      expect(pills(container)[0].textContent).toContain("copyGroups.badge {\"hidden\":2}")

      fireEvent.click(pills(container)[0])

      expect(tickedCards(container)).toEqual(["Echo Release"])
      expect(pills(container)[0].textContent).toContain("copyGroups.badgeTicked {\"hidden\":2,\"count\":1}")
      expect(pausedHashes(view)).toEqual(["hash-plain", "hash-third"])
    })

    it("shows no ticks on the pill in Select all mode", () => {
      const view = renderGroups()
      longPress(cardOf(view.container, "Delta Release"))

      fireEvent.click(view.getByRole("button", { name: "detailsPanel.selectAll" }))

      expect(tickedCards(view.container)).toEqual(["Delta Release", "Echo Release"])
      expect(pills(view.container)[0].textContent).toContain("copyGroups.badge {\"hidden\":2}")
      expect(pausedHashes(view)).toBeUndefined()
    })
  })

  it("opens and closes all groups from Expand all in the sort menu", async () => {
    const view = renderGroups()

    await pressExpandAll(view)
    expect(cardNames(view.container)).toEqual(["Delta Release", "Delta Release copy B", "Delta Release copy C", "Echo Release"])
    expect(await sortMenuItem(view, "menuitem", "copyGroups.collapseAll")).toBeTruthy()

    await pressExpandAll(view)
    expect(cardNames(view.container)).toEqual(["Delta Release", "Echo Release"])
  })
})

describe("TorrentCardsMobile Compact card", () => {
  // 2026-01-01 12:30 UTC and 2026-01-02 08:15 UTC. Now is 2026-01-02 10:15 UTC.
  const ADDED_ON = 1767270600
  const LATER_ADDED_ON = 1767341700
  const NOW = 1767348900
  const GIB = 1024 ** 3

  beforeEach(() => {
    localStorage.setItem(VIEW_MODE_KEY, "compact")
    // The card shows a relative time with every date setting.
    localStorage.setItem(DATE_TIME_KEY, JSON.stringify({ timezone: "UTC", timeFormat: "24h", dateFormat: "iso" }))
    vi.useFakeTimers({ toFake: ["Date"] })
    vi.setSystemTime(NOW * 1000)
  })

  const cardText = (container: HTMLElement, name: string) => cardOf(container, name).closest(".cursor-pointer")?.textContent ?? ""

  it("shows the size and how long ago the torrent was added on a complete idle torrent, and no percent", () => {
    const { container } = renderCards([
      makeTorrent({ name: "Foxtrot Release", progress: 1, size: 2 * GIB, downloaded: 2 * GIB, state: "stalledUP", added_on: ADDED_ON }),
    ])

    const text = cardText(container, "Foxtrot Release")
    expect(text).toContain("mobileCards.added {\"time\":\"21 hours ago\"}")
    expect(text).toContain(formatBytes(2 * GIB))
    expect(text).not.toContain(" / ")
    expect(text).not.toContain("%")
  })

  it("shows the upload speed on a complete seeding torrent, and no percent and no added time", () => {
    const { container } = renderCards([
      makeTorrent({ name: "Hotel Release", progress: 1, size: 2 * GIB, downloaded: 2 * GIB, upspeed: 1024, state: "uploading", added_on: ADDED_ON }),
    ])

    const text = cardText(container, "Hotel Release")
    expect(text).toContain("↑")
    expect(text).not.toContain("%")
    expect(text).not.toContain("mobileCards.added")
  })

  it("shows the downloaded size, the size, and the percent on an incomplete torrent, and no added time", () => {
    const { container } = renderCards([
      makeTorrent({ name: "Golf Release", progress: 0.5, size: 2 * GIB, downloaded: GIB, state: "pausedDL", added_on: ADDED_ON }),
    ])

    const text = cardText(container, "Golf Release")
    expect(text).toContain(`${formatBytes(GIB)} / ${formatBytes(2 * GIB)}`)
    expect(text).toContain("50%")
    expect(text).not.toContain("mobileCards.added")
  })

  it("shows the added time of the first-sorted member on the card of a closed group sorted by added time", () => {
    localStorage.setItem(SORT_KEY, JSON.stringify({ field: "added_on", order: "desc" }))
    const complete = { progress: 1, state: "stalledUP" as const }
    const { container } = renderGroups(copyGroupRows({
      top: { ...complete, added_on: ADDED_ON },
      sorted: { ...complete, added_on: LATER_ADDED_ON },
      third: { ...complete, added_on: ADDED_ON },
    }))

    expect(cardText(container, "Delta Release")).toContain("mobileCards.added {\"time\":\"2 hours ago\"}")
  })
})

describe("TorrentCardsMobile count bar", () => {
  beforeEach(() => {
    scenario.hasLoadedAll = false
    scenario.totalCount = 500
  })

  it("counts the torrents in memory on a grouped list, not the cards", () => {
    const { container } = renderGroups()

    expect(container.textContent).toContain("statusBar.torrentsLoaded {\"loaded\":4,\"total\":500}")
  })

  it("counts the torrents in memory on a flat list, not the load-more window", () => {
    const { container } = renderCards(flatRows(150))

    expect(container.textContent).toContain("statusBar.torrentsLoaded {\"loaded\":150,\"total\":500}")
  })

  it("shows the Grouping hint in place of the count while the ready field is false", () => {
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    scenario.copyGroupsReady = false
    const { container } = renderCards(flatRows(3))

    expect(container.textContent).toContain("copyGroups.grouping")
    expect(container.textContent).not.toContain("statusBar.torrentsLoaded")
  })

  it("shows the count and no hint for a response with no ready field", () => {
    localStorage.setItem(GROUP_COPIES_KEY, "true")
    const { container } = renderCards(flatRows(3))

    expect(container.textContent).not.toContain("copyGroups.grouping")
    expect(container.textContent).toContain("statusBar.torrentsLoaded {\"loaded\":3,\"total\":500}")
  })
})
