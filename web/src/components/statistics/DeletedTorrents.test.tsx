/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { focusManager, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { DELETES_PAGE, DeletedTorrents } from "@/components/statistics/DeletedTorrents"
import { TooltipProvider } from "@/components/ui/tooltip"
import { server } from "@/test/msw/server"
import { stubPhone } from "@/test/stubPhone"
import type { TransferHistoryCauseKind, TransferHistoryDeleteEvent, TransferHistoryInstance } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string, options?: Record<string, unknown>) => {
    const values = Object.entries(options ?? {})
    return values.length > 0 ? `${key} ${values.map(([name, value]) => `${name}=${String(value)}`).join(" ")}` : key
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
    trackerIcons: { data: undefined } as { data?: Record<string, string> },
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.trackerIcons }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => ({ data: [] }) }))
vi.mock("@/lib/incognito", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/incognito")>(),
  useIncognitoMode: () => mocks.incognitoMode,
}))

const OTHER_KINDS: TransferHistoryCauseKind[] = ["backupRestore", "clientProxy", "outside"]

interface DeletesRequest {
  instanceId: number
  cause: string | null
  offset: number
}

let deletesOf: Map<number, TransferHistoryDeleteEvent[]>
let requests: DeletesRequest[]
// While set, the route waits for it before it answers.
let hold: Promise<void> | undefined
// Answers the held requests. The teardown calls it too, so a test that fails before it answers them still ends.
let release: () => void
// The client of the rendered table. A test ends only after its requests answered, as a request that is still on its way
// reaches the route of the next test.
let client: QueryClient | undefined

function event(instanceId: number, index: number, time: number, kind: TransferHistoryCauseKind, name?: string): TransferHistoryDeleteEvent {
  return {
    time: new Date(time).toISOString(),
    hash: `${instanceId}-${index}`,
    name: `Built.Name.${instanceId}.${index}-GRP`,
    trackerGroup: "Aurora",
    uploaded: 1024,
    downloaded: 512,
    ratio: 2,
    seedingSeconds: 3600,
    cause: { kind, name },
  }
}

function instance(instanceId: number, name: string): TransferHistoryInstance {
  return { instanceId, name, firstRecordedAt: new Date(0).toISOString() }
}

function renderTable(instances: TransferHistoryInstance[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client = queryClient
  const table = (to: number) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <DeletedTorrents scope="30d" instances={instances} period={{ from: new Date(0), to: new Date(to) }} displayGroup={group => `group:${group}`} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const view = render(table(1e12))
  return { queryClient, moveTo: (to: number) => view.rerender(table(to)) }
}

function shownNames(): string[] {
  // The name is the first line of the cell. A phone row puts it in a button when the row opens, and in a div when it has nothing more to show.
  return [...document.querySelectorAll("tbody tr td:nth-child(2) > :first-child")].map(node => node.textContent ?? "")
}

// The names of ten deletes of an instance in a row, from the delete with the index first.
function tenNames(instanceId: number, first: number): string[] {
  return Array.from({ length: 10 }, (_, index) => `Built.Name.${instanceId}.${first + index}-GRP`)
}

function nextPage(): HTMLButtonElement {
  return screen.getByLabelText<HTMLButtonElement>("table.nextPage")
}

function previousPage(): HTMLButtonElement {
  return screen.getByLabelText<HTMLButtonElement>("table.previousPage")
}

beforeEach(() => {
  mocks.incognitoMode = [false, () => {}]
  mocks.trackerIcons = { data: undefined }
  requests = []
  hold = undefined
  release = () => {}
  deletesOf = new Map()
  server.use(http.get("*/api/instances/:id/transfer-history/deletes", async ({ params, request }) => {
    const url = new URL(request.url)
    const instanceId = Number(params.id)
    const cause = url.searchParams.get("cause")
    const offset = Number(url.searchParams.get("offset"))
    const limit = Number(url.searchParams.get("limit"))
    requests.push({ instanceId, cause, offset })
    await hold
    const all = deletesOf.get(instanceId) ?? []
    const kindsOf = (filter: string | null) => filter === null ? undefined : filter === "other" ? OTHER_KINDS : [filter]
    const kinds = kindsOf(cause)
    const matching = all.filter(event => !kinds || kinds.includes(event.cause.kind))
    const count = (filter: string) => all.filter(event => kindsOf(filter)!.includes(event.cause.kind)).length
    return HttpResponse.json({
      deletes: matching.slice(offset, offset + limit),
      counts: { automation: count("automation"), user: count("user"), other: count("other") },
    })
  }))
})

afterEach(async () => {
  release()
  try {
    await vi.waitFor(() => expect(client?.isFetching() ?? 0).toBe(0))
  } finally {
    client = undefined
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    // A test of Show fewer stubs it, as jsdom has no layout.
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView
  }
})

describe("DeletedTorrents", () => {
  it("shows the count of each cause and limits the rows to the selected cause", async () => {
    deletesOf.set(1, [
      event(1, 0, 5_000, "automation", "Remove old"),
      event(1, 1, 4_000, "user"),
      event(1, 2, 3_000, "clientProxy", "sonarr key"),
      event(1, 3, 2_000, "outside"),
    ])
    deletesOf.set(2, [event(2, 0, 4_500, "automation", "Ratio rule")])
    renderTable([instance(1, "Seedbox"), instance(2, "Home")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    const trigger = (labelKey: string) => screen.getByText(labelKey).closest("button")!
    expect(trigger("deletes.filters.all").textContent).toBe("deletes.filters.all5")
    expect(trigger("deletes.filters.automation").textContent).toBe("deletes.filters.automation2")
    expect(trigger("deletes.filters.user").textContent).toBe("deletes.filters.user1")
    expect(trigger("deletes.filters.other").textContent).toBe("deletes.filters.other2")
    expect(screen.getAllByText("· Remove old").length).toBeGreaterThan(0)
    expect(screen.getAllByText("· sonarr key").length).toBeGreaterThan(0)
    expect(screen.getAllByText("group:Aurora · Seedbox").length).toBeGreaterThan(0)

    fireEvent.mouseDown(trigger("deletes.filters.automation"), { button: 0 })
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.0-GRP", "Built.Name.2.0-GRP"]))
    expect(new Set(requests.filter(request => request.cause === "automation").map(request => request.instanceId))).toEqual(new Set([1, 2]))

    fireEvent.mouseDown(trigger("deletes.filters.other"), { button: 0 })
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.2-GRP", "Built.Name.1.3-GRP"]))
  })

  it("shows ten deletes for each page on a desktop, merges the instances with the newest first, and loads the next server page when a page needs it", async () => {
    // Instance 2 has the newer deletes, so the first pages of the table hold only them.
    deletesOf.set(1, Array.from({ length: 40 }, (_, index) => event(1, index, 500_000 - index * 10, "user")))
    deletesOf.set(2, Array.from({ length: 70 }, (_, index) => event(2, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox"), instance(2, "Home")])

    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(2, 0)))
    expect(screen.getByText("table.range from=1 to=10 total=110")).toBeTruthy()
    expect(previousPage().disabled).toBe(true)

    // The first five pages come from the first server page of each instance.
    for (const first of [10, 20, 30, 40]) {
      fireEvent.click(nextPage())
      expect(shownNames()).toEqual(tenNames(2, first))
    }
    expect(requests.filter(request => request.offset === DELETES_PAGE)).toHaveLength(0)

    fireEvent.click(nextPage())
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(2, 50)))
    expect(requests.filter(request => request.offset === DELETES_PAGE).map(request => request.instanceId).sort()).toEqual([1, 2])
    expect(screen.getByText("table.range from=51 to=60 total=110")).toBeTruthy()

    fireEvent.click(nextPage())
    fireEvent.click(nextPage())
    expect(shownNames()).toEqual(tenNames(1, 0))
    fireEvent.click(previousPage())
    expect(shownNames()).toEqual(tenNames(2, 60))

    for (let step = 0; step < 4; step++) {
      fireEvent.click(nextPage())
    }
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 30)))
    expect(screen.getByText("table.range from=101 to=110 total=110")).toBeTruthy()
    expect(nextPage().disabled).toBe(true)
  })

  it("goes back to the first page on a desktop when the user picks another cause, and stays on it after a return to the first cause", async () => {
    deletesOf.set(1, [
      ...Array.from({ length: 15 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")),
      ...Array.from({ length: 15 }, (_, index) => event(1, 100 + index, 500_000 - index * 10, "automation")),
    ])
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 0)))
    fireEvent.click(nextPage())
    expect(screen.getByText("table.range from=11 to=20 total=30")).toBeTruthy()

    fireEvent.mouseDown(screen.getByText("deletes.filters.automation").closest("button")!, { button: 0 })

    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 100)))
    expect(screen.getByText("table.range from=1 to=10 total=15")).toBeTruthy()

    fireEvent.mouseDown(screen.getByText("deletes.filters.all").closest("button")!, { button: 0 })

    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 0)))
    expect(screen.getByText("table.range from=1 to=10 total=30")).toBeTruthy()
  })

  it("shows the count of a desktop list of one page without the page buttons", async () => {
    deletesOf.set(1, Array.from({ length: 3 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(3))
    expect(screen.getByText("table.range from=1 to=3 total=3")).toBeTruthy()
    expect(screen.queryByLabelText("table.nextPage")).toBeNull()
  })

  // regression: #4132
  it("shows a delete once when a new delete moves it onto the next page", async () => {
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 0)))

    // Two new deletes before the next server page push the last two rows of the first server page onto the second one.
    deletesOf.set(1, [event(1, 100, 2_000_000, "user"), event(1, 101, 1_999_000, "user"), ...deletesOf.get(1)!])
    const names = shownNames()
    for (const first of [10, 20, 30, 40]) {
      fireEvent.click(nextPage())
      expect(shownNames()).toEqual(tenNames(1, first))
      names.push(...shownNames())
    }
    fireEvent.click(nextPage())
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 50)))
    names.push(...shownNames())
    expect(new Set(names).size).toBe(60)
  })

  // regression: #4132
  it("keeps the rows while the rows of a moved period load", async () => {
    deletesOf.set(1, [event(1, 0, 5_000, "user")])
    const { moveTo } = renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.0-GRP"]))

    hold = new Promise(resolve => {
      release = resolve
    })
    moveTo(1e12 + 300_000)
    await vi.waitFor(() => expect(requests.filter(request => request.cause === null)).toHaveLength(2))
    expect(shownNames()).toEqual(["Built.Name.1.0-GRP"])

    deletesOf.set(1, [event(1, 1, 6_000, "user"), event(1, 0, 5_000, "user")])
    release()
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.1-GRP", "Built.Name.1.0-GRP"]))
  })

  // regression: #4132
  it("lets the user load a page again after it failed", async () => {
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    let failNext = true
    server.use(http.get("*/api/instances/:id/transfer-history/deletes", ({ request }) => {
      if (new URL(request.url).searchParams.get("offset") === String(DELETES_PAGE) && failNext) {
        failNext = false
        return HttpResponse.json({ error: "database is locked" }, { status: 500 })
      }
      return undefined
    }))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 0)))

    for (let step = 0; step < 5; step++) {
      fireEvent.click(nextPage())
    }
    await vi.waitFor(() => expect(screen.getByText(/database is locked/)).toBeTruthy())
    // The table stays on the last page that it has the rows of.
    expect(shownNames()).toEqual(tenNames(1, 40))
    await vi.waitFor(() => expect(nextPage().disabled).toBe(false))
    fireEvent.click(nextPage())
    await vi.waitFor(() => expect(shownNames()).toEqual(tenNames(1, 50)))
  })

  // regression: #4179
  it("shows the deletes that a phone loaded on Show more, also after a reload failed", async () => {
    stubPhone("upright")
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    let failing = false
    server.use(http.get("*/api/instances/:id/transfer-history/deletes", () => failing ? HttpResponse.json({ error: "database is locked" }, { status: 500 }) : undefined))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    failing = true
    // A window that gets the focus again loads the page again.
    act(() => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
    })
    await vi.waitFor(() => expect(screen.getByText(/database is locked/)).toBeTruthy())

    fireEvent.click(screen.getByText("deletes.more"))

    expect(shownNames()).toHaveLength(15)
  })

  // regression: #4179
  it("shows five deletes on a phone, ten more for each Show more, and loads the next page after the loaded ones", async () => {
    stubPhone("upright")
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    expect(screen.getByText("deletes.shown shown=5 total=60")).toBeTruthy()
    for (const shown of [15, 25, 35, 45]) {
      fireEvent.click(screen.getByText("deletes.more"))
      await vi.waitFor(() => expect(shownNames()).toHaveLength(shown))
    }
    expect(requests.filter(request => request.offset === DELETES_PAGE)).toHaveLength(0)

    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(shownNames()).toHaveLength(55))
    expect(requests.filter(request => request.offset === DELETES_PAGE)).toHaveLength(1)
    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(shownNames()).toHaveLength(60))
    expect(screen.queryByText("deletes.more")).toBeNull()
  })

  // regression: #4179
  it("shows the next loaded deletes on a phone at once, also while the loaded pages load again", async () => {
    stubPhone("upright")
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    hold = new Promise(resolve => { release = resolve })
    // A window that gets the focus again loads the page again.
    act(() => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
    })
    await vi.waitFor(() => expect(requests.filter(request => request.cause === null)).toHaveLength(2))

    fireEvent.click(screen.getByText("deletes.more"))

    expect(shownNames()).toHaveLength(15)
    release()
  })

  // regression: #4179
  it("shows five deletes again on a phone after Show fewer, and keeps its button in view", async () => {
    stubPhone("upright")
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    expect(screen.queryByText("table.showFewer")).toBeNull()
    fireEvent.click(screen.getByText("deletes.more"))
    fireEvent.click(screen.getByText("deletes.more"))
    expect(shownNames()).toHaveLength(25)
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView

    fireEvent.click(screen.getByText("table.showFewer"))

    expect(shownNames()).toHaveLength(5)
    expect(scrollIntoView).toHaveBeenCalledOnce()
    expect(screen.queryByText("table.showFewer")).toBeNull()
  })

  // regression: #4179
  it("loads the first page of the other filters of each instance after the first filter", async () => {
    deletesOf.set(1, [event(1, 0, 5_000, "user")])
    deletesOf.set(2, [event(2, 0, 4_000, "automation")])
    const { queryClient } = renderTable([instance(1, "Seedbox"), instance(2, "Home")])

    await vi.waitFor(() => expect(requests.filter(request => request.cause !== null).map(request => `${request.instanceId} ${request.cause} ${request.offset}`).sort()).toEqual([
      "1 automation 0", "1 other 0", "1 user 0", "2 automation 0", "2 other 0", "2 user 0",
    ]))
    // The rows of a switch come from the preload only when each preload answered.
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0))
    const trigger = (labelKey: string) => screen.getByText(labelKey).closest("button")!
    fireEvent.mouseDown(trigger("deletes.filters.user"), { button: 0 })

    expect(shownNames()).toEqual(["Built.Name.1.0-GRP"])
  })

  // regression: #4179
  it("keeps the rows of the last filter, dimmed, until the rows of the next filter load", async () => {
    deletesOf.set(1, [event(1, 0, 5_000, "user"), event(1, 1, 4_000, "automation")])
    let preloading = false
    const automation = new Promise<void>(resolve => {
      release = resolve
    })
    server.use(http.get("*/api/instances/:id/transfer-history/deletes", async ({ request }) => {
      if (new URL(request.url).searchParams.get("cause") === "automation") {
        preloading = true
        await automation
      }
      return undefined
    }))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(2))
    // The preload of the filter Automations still waits, so a switch has no rows of its own yet.
    await vi.waitFor(() => expect(preloading).toBe(true))
    const trigger = (labelKey: string) => screen.getByText(labelKey).closest("button")!

    fireEvent.mouseDown(trigger("deletes.filters.automation"), { button: 0 })

    expect(shownNames()).toHaveLength(2)
    expect(document.querySelector("table")!.closest("[aria-busy]")!.getAttribute("aria-busy")).toBe("true")
    release()
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.1-GRP"]))
    expect(document.querySelector("table")!.closest("[aria-busy]")!.getAttribute("aria-busy")).toBe("false")
  })

  // regression: #4179
  it("shows five deletes again on a phone after a return to an earlier filter", async () => {
    stubPhone("upright")
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(5))
    fireEvent.click(screen.getByText("deletes.more"))
    expect(shownNames()).toHaveLength(15)
    const trigger = (labelKey: string) => screen.getByText(labelKey).closest("button")!

    fireEvent.mouseDown(trigger("deletes.filters.user"), { button: 0 })
    // The rows of the other filter show. Until they load, the earlier rows stay and keep their count.
    await vi.waitFor(() => expect(document.querySelector("table")!.closest("[aria-busy]")!.getAttribute("aria-busy")).toBe("false"))
    fireEvent.mouseDown(trigger("deletes.filters.all"), { button: 0 })

    await vi.waitFor(() => expect(trigger("deletes.filters.all").getAttribute("aria-selected")).toBe("true"))
    expect(shownNames()).toHaveLength(5)
  })

  // regression: #4179
  it("opens a delete with a cut name on a tap anywhere on its row on a phone, and closes it on the next tap", async () => {
    stubPhone("upright")
    // jsdom has no layout. A name that needs more lines than the row shows has a larger scroll height.
    vi.spyOn(Element.prototype, "scrollHeight", "get").mockReturnValue(60)
    vi.spyOn(Element.prototype, "clientHeight", "get").mockReturnValue(40)
    deletesOf.set(1, [event(1, 0, 5_000, "user")])
    renderTable([instance(1, "Seedbox")])
    const name = (await screen.findByText("Built.Name.1.0-GRP")).closest("button")!
    expect(name.getAttribute("aria-expanded")).toBe("false")

    fireEvent.click(name)
    expect(name.getAttribute("aria-expanded")).toBe("true")

    fireEvent.click(name.closest("td")!.querySelector("div")!)
    expect(name.getAttribute("aria-expanded")).toBe("false")
  })

  // regression: #4179
  it("offers no opener on a phone for a delete whose name and cause show in full", async () => {
    stubPhone("upright")
    deletesOf.set(1, [event(1, 0, 5_000, "automation", "Tidy up")])
    renderTable([instance(1, "Seedbox")])
    const name = await screen.findByText("Built.Name.1.0-GRP")

    expect(name.closest("tr")!.querySelector("[aria-expanded]")).toBeNull()
    fireEvent.click(name)
    expect(name.closest("tr")!.querySelector("[aria-expanded]")).toBeNull()
  })

  // regression: #4358
  it("shows a cut-off cause on a tap on a touch screen wider than a phone, where the row does not open", async () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    try {
      vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
      vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
      deletesOf.set(1, [event(1, 0, 5_000, "automation", "Tidy up")])
      renderTable([instance(1, "Seedbox")])
      await vi.waitFor(() => expect(screen.getAllByText("· Tidy up").at(-1)!.getAttribute("data-state")).toBe("closed"))
      const cause = screen.getAllByText("· Tidy up").at(-1)!

      fireEvent.pointerDown(cause, { pointerType: "touch" })
      fireEvent.pointerUp(cause, { pointerType: "touch" })
      fireEvent.click(cause)

      expect(screen.getByRole("tooltip").textContent).toContain("· Tidy up")
    } finally {
      delete touchWindow.ontouchstart
    }
  })

  it("shows the tracker icon next to the tracker of each delete, the first letter for a tracker with no icon, and none for Public", async () => {
    mocks.trackerIcons = { data: { "a.test": "data:image/png;base64,YQ==" } }
    deletesOf.set(1, [
      { ...event(1, 0, 7_000, "user"), trackerGroup: "a.test" },
      { ...event(1, 1, 6_000, "user"), trackerGroup: "c.test" },
      { ...event(1, 2, 5_000, "user"), trackerGroup: "Public" },
    ])
    renderTable([instance(1, "Seedbox")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(3))
    const trackerCells = [...document.querySelectorAll("tbody tr td:nth-child(3)")]
    expect(trackerCells.map(cell => cell.querySelector("img")?.getAttribute("src") ?? cell.querySelector("span[aria-hidden=true]")?.textContent ?? null)).toEqual(["data:image/png;base64,YQ==", "C", null])
  })

  it("masks the torrent names in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    deletesOf.set(1, [event(1, 0, 5_000, "user")])
    renderTable([instance(1, "Seedbox")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(1))
    expect(shownNames()[0]).not.toBe("Built.Name.1.0-GRP")
    expect(document.body.textContent).not.toContain("Built.Name.1.0-GRP")
  })
})
