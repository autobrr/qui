/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { getLinuxIsoName, getLinuxSavePath } from "@/lib/incognito"
import type { TransferDetail, TransferList, TransferRow } from "@/lib/transfers-api"
import { server } from "@/test/msw/server"

import { TransfersTab } from "./TransfersTab"

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))

const screenSize = vi.hoisted(() => ({ phone: false }))
vi.mock("@/hooks/useMediaQuery", () => ({
  SIDEWAYS_PHONE_QUERY: "sideways",
  useIsPhone: () => screenSize.phone,
  useIsMobile: () => screenSize.phone,
  useMediaQuery: () => false,
}))

const incognito = vi.hoisted(() => ({ on: false }))
vi.mock("@/lib/incognito", async importOriginal => {
  const off: [boolean, (value: boolean) => void] = [false, () => {}]
  const on: [boolean, (value: boolean) => void] = [true, () => {}]
  return { ...await importOriginal<typeof import("@/lib/incognito")>(), useIncognitoMode: () => incognito.on ? on : off }
})

const finished = "2026-11-17T11:00:00Z"

function row(edit: Partial<TransferRow>): TransferRow {
  return {
    id: 1, kind: "transfer", way: "sftp", state: "waiting", startedBy: "user", ruleId: null, ruleName: "",
    sourceInstanceId: 1, targetInstanceId: 2, hash: "aaaa", name: "Show.S01.1080p-GRP", torrents: 1,
    after: "removeWithFiles", leaveStopped: false, retryAt: null, failureClass: "", failureReason: "",
    createdAt: "2026-11-17T10:00:00Z", finishedAt: null, hints: [], progress: null,
    ...edit,
  }
}

const copying = row({
  id: 1, name: "Show.S01.1080p-GRP", state: "copying", torrents: 3,
  progress: { done: 512 * 1024 * 1024, total: 1024 * 1024 * 1024, speed: 1024 * 1024 },
})
const failed = row({
  id: 2, name: "Film.2019.2160p-ABC", state: "failed", way: "bittorrent", startedBy: "rule", ruleId: 7, ruleName: "Move old seeds",
  failureClass: "permanent", failureReason: "A file on the target does not match.", finishedAt: finished,
  hints: [{ kind: "tcpAndUtp", instanceId: 2 }],
})
const exported = row({ id: 3, kind: "export", way: "", name: "Album.2020.FLAC-XYZ", state: "done", finishedAt: finished, startedBy: "rule", ruleId: 9 })
const waitingRetry = row({ id: 4, name: "Doc.2018.720p-QRS", state: "failed", failureClass: "temporary",
  failureReason: "homelab does not answer.", retryAt: "2099-01-01T00:00:00Z" })

const counts = { active: 1, waiting: 0, finished: 1, failed: 2 }
let requests: string[]

function listOf(filter: string | null): TransferList {
  const rows = [copying, failed, exported, waitingRetry]
  const chip = { copying: "active", failed: "failed", done: "finished" } as Record<string, string>
  return { rows: filter ? rows.filter(r => chip[r.state] === filter) : rows, counts }
}

const detail: TransferDetail = {
  ...failed,
  steps: [
    { name: "start", at: "2026-11-17T10:00:00Z", detail: "" },
    { name: "names", at: "2026-11-17T10:00:04Z", detail: "[[\"Film.2019.2160p-ABC.mkv\"]]" },
    { name: "pass by BitTorrent", at: "2026-11-17T10:00:05Z", detail: "" },
    { name: "teleport", at: "2026-11-17T10:00:06Z", detail: "" },
    { name: "failed", at: "2026-11-17T10:00:07Z", detail: "" },
  ],
  log: [{ at: "2026-11-17T10:00:05Z", message: "Passing 70 B by a Transfer torrent from seedbox to homelab." }],
  members: [{ hash: "bbbb", name: "Film.2019.2160p-ABC", targetInstanceId: 2, savePath: "/srv/movies", category: "", tags: [] }],
}

beforeEach(() => {
  // The filter tabs and a tooltip measure their elements, and jsdom has no ResizeObserver.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  requests = []
  server.use(
    http.get("*/api/instances", () => HttpResponse.json([{ id: 1, name: "seedbox" }, { id: 2, name: "homelab" }])),
    http.get("*/api/transfers", ({ request }) => {
      const filter = new URL(request.url).searchParams.get("filter")
      requests.push(`GET /api/transfers${filter ? `?filter=${filter}` : ""}`)
      return HttpResponse.json(listOf(filter))
    }),
    http.get("*/api/transfers/:id", () => HttpResponse.json(detail)),
    http.post("*/api/transfers/:id/retry", ({ params }) => {
      requests.push(`POST /api/transfers/${params.id}/retry`)
      return HttpResponse.json({ id: 10 }, { status: 202 })
    }),
    http.post("*/api/transfers/:id/cancel", ({ params }) => {
      requests.push(`POST /api/transfers/${params.id}/cancel`)
      return new HttpResponse(null, { status: 202 })
    }),
    http.delete("*/api/transfers/:id", ({ params }) => {
      requests.push(`DELETE /api/transfers/${params.id}`)
      return new HttpResponse(null, { status: 204 })
    })
  )
})

afterEach(() => {
  screenSize.phone = false
  incognito.on = false
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

function renderTab() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <TransfersTab />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

async function rowOf(name: string): Promise<HTMLElement> {
  const cell = await screen.findByText(name)
  const tr = cell.closest("tr")
  if (!tr) throw new Error(`no row of ${name}`)
  return tr
}

const buttonNames = (tr: HTMLElement) => within(tr).queryAllByRole("button").map(button => button.getAttribute("aria-label"))

async function openDetails(name: string): Promise<HTMLElement> {
  const tr = await rowOf(name)
  if (screenSize.phone) {
    // A phone puts the row actions in a menu.
    fireEvent.pointerDown(within(tr).getByRole("button", { name: "More actions" }), { button: 0, ctrlKey: false })
    fireEvent.click(await screen.findByRole("menuitem", { name: "Details" }))
  } else {
    fireEvent.click(within(tr).getByRole("button", { name: "Details" }))
  }
  const dialog = await screen.findByRole("dialog")
  await within(dialog).findByRole("tablist")
  return dialog
}

/** Radix selects a tab on the press of the main button. */
const openTab = (dialog: HTMLElement, name: RegExp) => fireEvent.mouseDown(within(dialog).getByRole("tab", { name }), { button: 0 })
const selectedTab = (dialog: HTMLElement) => within(dialog).getByRole("tab", { selected: true }).textContent
const stepLabels = (dialog: HTMLElement) =>
  within(within(dialog).getByRole("tabpanel")).getAllByRole("listitem").map(item => item.querySelector("div > span")?.textContent)

describe("the Transfers tab", () => {
  it("shows each row with what it allows, and lists the rows of a filter, with the count of each filter", async () => {
    renderTab()
    const tr = await rowOf("Show.S01.1080p-GRP")
    const text = tr.textContent ?? ""
    expect(text).toContain("+2 cross-seeds")
    expect(text).toContain("seedbox → homelab")
    expect(text).toContain("Copy over SFTP")
    // 512 MiB left at 1 MiB/s.
    expect(text).toContain("50% · 1 MiB/s · 8m left")
    expect(text).toContain("Copying")
    expect(text).toContain("By you")
    expect(buttonNames(tr)).toEqual(["Details", "Cancel"])

    const failedRow = await rowOf("Film.2019.2160p-ABC")
    expect(failedRow.textContent).toContain("A file on the target does not match.")
    expect(failedRow.textContent).toContain("By rule Move old seeds")
    expect(failedRow.textContent).toContain("homelab uses qBittorrent's default \"TCP and μTP\"")
    expect(buttonNames(failedRow)).toEqual(["Details", "Retry", "Remove"])

    const exportRow = await rowOf("Album.2020.FLAC-XYZ")
    expect(exportRow.textContent).toContain("Export")
    expect(exportRow.textContent).toContain("By a rule that is gone")
    // An Export copies nothing, so the API sends an empty way and the row names none.
    expect(exportRow.textContent).not.toContain("Unknown way")
    expect(buttonNames(exportRow)).toEqual(["Details", "Remove"])

    const retryRow = await rowOf("Doc.2018.720p-QRS")
    expect(retryRow.textContent).toMatch(/Retry in \d+ years?/)
    expect(buttonNames(retryRow)).toEqual(["Details", "Retry", "Cancel"])

    // Each filter shows its label and its count; All counts every row.
    const filters = within(screen.getByRole("tablist", { name: "Show" })).getAllByRole("tab")
      .map(filter => Array.from(filter.childNodes, node => node.textContent))
    expect(filters).toEqual([["All", "4"], ["Active", "1"], ["Waiting", "0"], ["Finished", "1"], ["Failed", "2"]])

    const failedFilter = screen.getByRole("tab", { name: /^Failed/ })
    fireEvent.mouseDown(failedFilter, { button: 0 })
    await waitFor(() => expect(screen.queryByText("Show.S01.1080p-GRP")).toBeNull())
    expect(screen.getByText("Film.2019.2160p-ABC")).toBeTruthy()
    expect(requests).toContain("GET /api/transfers?filter=failed")
    expect(failedFilter.getAttribute("aria-selected")).toBe("true")
  })

  it("splits a long list into pages of 50 rows, and keeps the counts of every row", async () => {
    const long = { active: 0, waiting: 120, finished: 0, failed: 0 }
    server.use(http.get("*/api/transfers", ({ request }) => {
      const query = new URL(request.url).searchParams
      const offset = Number(query.get("offset"))
      requests.push(`GET /api/transfers?${query}`)
      const rows = Array.from({ length: Math.min(50, long.waiting - offset) }, (_, i) =>
        row({ id: 1000 - offset - i, hash: `h${offset + i}`, name: `Built.Show.E${offset + i + 1}.1080p-GRP` }))
      return HttpResponse.json({ rows, counts: long })
    }))
    // jsdom has no layout, so it has no scrollIntoView.
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    try {
      renderTab()
      const pages = await screen.findByRole("navigation", { name: "Pages of the list" })
      const previous = within(pages).getByRole("button", { name: "Previous" })
      const next = within(pages).getByRole("button", { name: "Next" })
      expect(within(pages).getByText("1–50 of 120")).toBeTruthy()
      expect(previous.hasAttribute("disabled")).toBe(true)
      expect(screen.getByText("Built.Show.E1.1080p-GRP")).toBeTruthy()

      fireEvent.click(next)
      await screen.findByText("Built.Show.E51.1080p-GRP")
      expect(within(pages).getByText("51–100 of 120")).toBeTruthy()
      expect(requests).toContain("GET /api/transfers?limit=50&offset=50")
      expect(scrollIntoView).toHaveBeenCalled()

      fireEvent.click(next)
      await screen.findByText("Built.Show.E120.1080p-GRP")
      expect(within(pages).getByText("101–120 of 120")).toBeTruthy()
      expect(next.hasAttribute("disabled")).toBe(true)

      // Another filter starts on its first page.
      fireEvent.mouseDown(screen.getByRole("tab", { name: /^Waiting/ }), { button: 0 })
      await waitFor(() => expect(requests).toContain("GET /api/transfers?limit=50&offset=0&filter=waiting"))
      expect(await within(pages).findByText("1–50 of 120")).toBeTruthy()
    } finally {
      delete (Element.prototype as Partial<Element>).scrollIntoView
    }
  })

  it("shows no pages when the rows fit on one page", async () => {
    renderTab()
    await rowOf("Film.2019.2160p-ABC")
    expect(screen.queryByRole("navigation", { name: "Pages of the list" })).toBeNull()
  })

  it("puts the actions of a row in one menu on a phone", async () => {
    screenSize.phone = true
    renderTab()
    const tr = await rowOf("Film.2019.2160p-ABC")
    const menu = within(tr).getByRole("button", { name: "More actions" })
    expect(menu.getAttribute("aria-describedby")).toBe(within(tr).getByText("Film.2019.2160p-ABC").parentElement?.id)
    expect(within(tr).getAllByRole("button")).toEqual([menu])

    fireEvent.pointerDown(menu, { button: 0, ctrlKey: false })
    const items = (await screen.findAllByRole("menuitem")).map(item => item.textContent)
    expect(items).toEqual(["Details", "Retry", "Remove"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Retry" }))
    await waitFor(() => expect(requests).toContain("POST /api/transfers/2/retry"))
  })

  it("retries and removes a row", async () => {
    renderTab()
    const tr = await rowOf("Film.2019.2160p-ABC")
    fireEvent.click(within(tr).getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(requests).toContain("POST /api/transfers/2/retry"))
    fireEvent.click(within(tr).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(requests).toContain("DELETE /api/transfers/2"))
    expect(toast.success).toHaveBeenCalledWith("The Transfer runs again.")
  })

  // regression: #4242
  it("sends one Retry for each row while the answers wait", async () => {
    let answer: () => void = () => {}
    const answered = new Promise<void>(resolve => { answer = resolve })
    server.use(http.post("*/api/transfers/:id/retry", async ({ params }) => {
      requests.push(`POST /api/transfers/${params.id}/retry`)
      await answered
      return HttpResponse.json({ id: 10 }, { status: 202 })
    }))
    renderTab()
    const first = within(await rowOf("Film.2019.2160p-ABC")).getByRole<HTMLButtonElement>("button", { name: "Retry" })
    const second = within(await rowOf("Doc.2018.720p-QRS")).getByRole<HTMLButtonElement>("button", { name: "Retry" })
    fireEvent.click(first)
    await waitFor(() => expect(first.disabled).toBe(true))
    fireEvent.click(second)
    await waitFor(() => expect(second.disabled).toBe(true))
    fireEvent.click(first)
    answer()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2))
    expect(requests.filter(r => r.endsWith("/retry"))).toEqual(["POST /api/transfers/2/retry", "POST /api/transfers/4/retry"])
  })

  // regression: #4242
  it("sends one Cancel and one Remove for a row while the answers wait, and keeps the Cancel question open until its answer", async () => {
    let answer: () => void = () => {}
    const answered = new Promise<void>(resolve => { answer = resolve })
    server.use(
      http.post("*/api/transfers/:id/cancel", async ({ params }) => {
        requests.push(`POST /api/transfers/${params.id}/cancel`)
        await answered
        // As qui's server sends it: no body, and a length of 0.
        return new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } })
      }),
      http.delete("*/api/transfers/:id", async ({ params }) => {
        requests.push(`DELETE /api/transfers/${params.id}`)
        await answered
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderTab()
    const remove = within(await rowOf("Film.2019.2160p-ABC")).getByRole<HTMLButtonElement>("button", { name: "Remove" })
    fireEvent.click(remove)
    await waitFor(() => expect(remove.disabled).toBe(true))
    fireEvent.click(remove)

    const cancel = within(await rowOf("Show.S01.1080p-GRP")).getByRole<HTMLButtonElement>("button", { name: "Cancel" })
    fireEvent.click(cancel)
    const dialog = await screen.findByRole("alertdialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel the Transfer" }))
    await waitFor(() => expect(requests).toContain("POST /api/transfers/1/cancel"))
    expect(screen.getByRole("alertdialog")).toBe(dialog)
    expect(cancel.disabled).toBe(true)
    answer()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(requests.filter(r => r.startsWith("DELETE") || r.endsWith("/cancel"))).toEqual(["DELETE /api/transfers/2", "POST /api/transfers/1/cancel"])
  })

  it("cancels a Transfer only after the user confirms", async () => {
    renderTab()
    fireEvent.click(within(await rowOf("Show.S01.1080p-GRP")).getByRole("button", { name: "Cancel" }))
    const dialog = await screen.findByRole("alertdialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Keep running" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(requests.filter(r => r.includes("cancel"))).toEqual([])

    fireEvent.click(within(await rowOf("Show.S01.1080p-GRP")).getByRole("button", { name: "Cancel" }))
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel the Transfer" }))
    await waitFor(() => expect(requests).toContain("POST /api/transfers/1/cancel"))
  })

  it("opens the Details dialog on Overview, with a short title, the name, the state, and the route", async () => {
    renderTab()
    const dialog = await openDetails("Film.2019.2160p-ABC")
    expect(within(dialog).getByRole("heading").textContent).toBe("Transfer details")
    expect(dialog.textContent).toContain("Film.2019.2160p-ABC")
    expect(dialog.textContent).toContain("seedbox → homelab")
    expect(selectedTab(dialog)).toBe("Overview")
    const overview = within(dialog).getByRole("tabpanel").textContent ?? ""
    expect(overview).toContain("Why it failed")
    expect(overview).toContain("A file on the target does not match.")
    expect(overview).toContain("By rule Move old seeds")
  })

  it("shows the torrents, the steps, and the log each in its own tab", async () => {
    renderTab()
    const dialog = await openDetails("Film.2019.2160p-ABC")
    openTab(dialog, /^Torrents/)
    const torrents = within(dialog).getByRole("tabpanel").textContent ?? ""
    expect(torrents).toContain("homelab")
    expect(torrents).toContain("/srv/movies")
    openTab(dialog, /^Log/)
    expect(within(dialog).getByRole("tabpanel").textContent).toContain("Passing 70 B by a Transfer torrent from seedbox to homelab.")
    openTab(dialog, /^Steps/)
    expect(within(dialog).getByRole("tabpanel").textContent).toContain("Passing by a Transfer torrent")
  })

  // regression: #4242
  it("keeps the step that records the file names out of the Details dialog", async () => {
    renderTab()
    const dialog = await openDetails("Film.2019.2160p-ABC")
    openTab(dialog, /^Steps/)
    expect(stepLabels(dialog)).toEqual(["Started", "Passing by a Transfer torrent", "Another step", "Failed"])
  })

  // regression: #4242
  it("names the torrent of a step that adds one, and shows no other step data", async () => {
    server.use(http.get("*/api/transfers/:id", () => HttpResponse.json({
      ...detail,
      steps: [
        ...detail.steps.slice(0, 3),
        { name: "place", at: "2026-11-17T10:00:06Z", detail: "[]" },
        { name: "added", at: "2026-11-17T10:00:07Z", detail: "bbbb" },
      ],
    })))
    renderTab()
    const dialog = await openDetails("Film.2019.2160p-ABC")
    openTab(dialog, /^Steps/)
    const steps = within(dialog).getByRole("tabpanel")
    const added = within(steps).getAllByRole("listitem").at(-1)!
    expect(added.textContent).toContain("Added to the target")
    expect(added.textContent).toContain("Film.2019.2160p-ABC")
    expect(steps.textContent).not.toContain("[]")
    expect(steps.textContent).not.toContain("bbbb")
    expect(steps.textContent).not.toContain("Film.2019.2160p-ABC.mkv")
  })

  it("puts the tabs beside the content on a wide screen, and above it on a phone", async () => {
    renderTab()
    let dialog = await openDetails("Film.2019.2160p-ABC")
    expect(within(dialog).getByRole("tablist").getAttribute("aria-orientation")).toBe("vertical")
    fireEvent.keyDown(dialog, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    cleanup()

    screenSize.phone = true
    renderTab()
    dialog = await openDetails("Film.2019.2160p-ABC")
    expect(within(dialog).getByRole("tablist").getAttribute("aria-orientation")).toBe("horizontal")
  })

  it("opens again on Overview after the user looked at another tab", async () => {
    renderTab()
    let dialog = await openDetails("Film.2019.2160p-ABC")
    openTab(dialog, /^Log/)
    expect(selectedTab(dialog)).toMatch(/^Log/)
    fireEvent.keyDown(dialog, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    dialog = await openDetails("Film.2019.2160p-ABC")
    expect(selectedTab(dialog)).toBe("Overview")
  })

  // regression: #4242
  it("shows made-up names in incognito mode, and hides the failure reason", async () => {
    incognito.on = true
    renderTab()
    await screen.findAllByText(getLinuxIsoName("aaaa"))
    const table = screen.getByRole("table").textContent ?? ""
    for (const name of ["Show.S01.1080p-GRP", "Film.2019.2160p-ABC", "Album.2020.FLAC-XYZ", "Doc.2018.720p-QRS"]) {
      expect(table).not.toContain(name)
    }
    expect(table).not.toContain("A file on the target does not match.")
    expect(table).not.toContain("homelab does not answer.")
    expect(table).toContain("Hidden in incognito mode")
  })

  // regression: #4242
  it("shows made-up names and save paths in the Details dialog in incognito mode, and hides the log and the step data", async () => {
    incognito.on = true
    server.use(http.get("*/api/transfers/:id", () => HttpResponse.json({
      ...detail,
      steps: [...detail.steps, { name: "place", at: "2026-11-17T10:00:06Z", detail: "[\"Film.2019.2160p-ABC/film.mkv\"]" }, { name: "added", at: "2026-11-17T10:00:07Z", detail: "bbbb" }],
      members: [{ ...detail.members[0], category: "movies-private", tags: ["tracker-tag"] }],
    })))
    renderTab()
    await screen.findAllByText(getLinuxIsoName("aaaa"))
    const tr = document.querySelector<HTMLElement>("[data-transfer-id='2']")!
    fireEvent.click(within(tr).getByRole("button", { name: "Details" }))
    const dialog = await screen.findByRole("dialog")
    await within(dialog).findByText(getLinuxIsoName("aaaa"))
    let text = within(dialog).getByRole("tabpanel").textContent ?? ""
    expect(text).toContain("Hidden in incognito mode")
    openTab(dialog, /^Torrents/)
    text = within(dialog).getByRole("tabpanel").textContent ?? ""
    expect(text).toContain(getLinuxIsoName("bbbb"))
    expect(text).toContain(getLinuxSavePath("bbbb"))
    openTab(dialog, /^Steps/)
    text = within(dialog).getByRole("tabpanel").textContent ?? ""
    expect(text).toContain("Placed the files")
    expect(text).toContain(getLinuxIsoName("bbbb"))
    openTab(dialog, /^Log/)
    expect(within(dialog).getByRole("tabpanel").textContent).toContain("Hidden in incognito mode")
    const all = dialog.textContent ?? ""
    for (const real of ["Film.2019.2160p-ABC", "/srv/movies", "A file on the target does not match.", "Passing 70 B", "movies-private", "tracker-tag"]) {
      expect(all).not.toContain(real)
    }
  })
})
