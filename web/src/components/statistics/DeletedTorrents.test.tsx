/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { DELETES_PAGE, DeletedTorrents } from "@/components/statistics/DeletedTorrents"
import { server } from "@/test/msw/server"
import type { TransferHistoryCauseKind, TransferHistoryDeleteEvent, TransferHistoryInstance } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string, options?: Record<string, unknown>) => {
    const values = Object.entries(options ?? {})
    return values.length > 0 ? `${key} ${values.map(([name, value]) => `${name}=${String(value)}`).join(" ")}` : key
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))
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
  const table = (to: number) => (
    <QueryClientProvider client={queryClient}>
      <DeletedTorrents scope="30d" instances={instances} period={{ from: new Date(0), to: new Date(to) }} displayGroup={group => `group:${group}`} />
    </QueryClientProvider>
  )
  const view = render(table(1e12))
  return { moveTo: (to: number) => view.rerender(table(to)) }
}

function shownNames(): string[] {
  return [...document.querySelectorAll("tbody tr td:nth-child(2) > span[title]")].map(node => node.getAttribute("title") ?? "")
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  mocks.incognitoMode = [false, () => {}]
  requests = []
  hold = undefined
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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
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

    fireEvent.mouseDown(trigger("deletes.filters.automation"), { button: 0 })
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.0-GRP", "Built.Name.2.0-GRP"]))
    expect(new Set(requests.filter(request => request.cause === "automation").map(request => request.instanceId))).toEqual(new Set([1, 2]))

    fireEvent.mouseDown(trigger("deletes.filters.other"), { button: 0 })
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.2-GRP", "Built.Name.1.3-GRP"]))
  })

  it("merges the pages of the instances with the newest first and loads the next page on request", async () => {
    // Instance 2 has the newer deletes, so the first page of the table holds only them.
    deletesOf.set(1, Array.from({ length: 40 }, (_, index) => event(1, index, 500_000 - index * 10, "user")))
    deletesOf.set(2, Array.from({ length: 70 }, (_, index) => event(2, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox"), instance(2, "Home")])

    await vi.waitFor(() => expect(shownNames()).toHaveLength(DELETES_PAGE))
    expect(shownNames()).toEqual(Array.from({ length: DELETES_PAGE }, (_, index) => `Built.Name.2.${index}-GRP`))
    expect(screen.getByText(`deletes.shown shown=${DELETES_PAGE} total=110`)).toBeTruthy()

    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(shownNames()).toHaveLength(2 * DELETES_PAGE))
    const names = shownNames()
    expect(names.slice(0, 70)).toEqual(Array.from({ length: 70 }, (_, index) => `Built.Name.2.${index}-GRP`))
    expect(names.slice(70)).toEqual(Array.from({ length: 30 }, (_, index) => `Built.Name.1.${index}-GRP`))
    expect(requests.filter(request => request.offset === DELETES_PAGE).map(request => request.instanceId).sort()).toEqual([1, 2])

    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(shownNames()).toHaveLength(110))
    expect(screen.queryByText("deletes.more")).toBeNull()
  })

  // regression: #4132
  it("shows a delete once when a new delete moves it onto the next page", async () => {
    deletesOf.set(1, Array.from({ length: 60 }, (_, index) => event(1, index, 1_000_000 - index * 10, "user")))
    renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toHaveLength(DELETES_PAGE))

    // Two new deletes before the next page push the last two rows of the first page onto the second page.
    deletesOf.set(1, [event(1, 100, 2_000_000, "user"), event(1, 101, 1_999_000, "user"), ...deletesOf.get(1)!])
    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(shownNames()).toHaveLength(60))
    expect(new Set(shownNames()).size).toBe(60)
  })

  // regression: #4132
  it("keeps the rows while the rows of a moved period load", async () => {
    deletesOf.set(1, [event(1, 0, 5_000, "user")])
    const { moveTo } = renderTable([instance(1, "Seedbox")])
    await vi.waitFor(() => expect(shownNames()).toEqual(["Built.Name.1.0-GRP"]))

    let release = () => {}
    hold = new Promise(resolve => {
      release = resolve
    })
    moveTo(1e12 + 300_000)
    await vi.waitFor(() => expect(requests).toHaveLength(2))
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
    await vi.waitFor(() => expect(shownNames()).toHaveLength(DELETES_PAGE))

    fireEvent.click(screen.getByText("deletes.more"))
    await vi.waitFor(() => expect(screen.getByText(/database is locked/)).toBeTruthy())
    const button = screen.getByText("deletes.more").closest("button")!
    await vi.waitFor(() => expect(button.disabled).toBe(false))
    fireEvent.click(button)
    await vi.waitFor(() => expect(shownNames()).toHaveLength(60))
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
