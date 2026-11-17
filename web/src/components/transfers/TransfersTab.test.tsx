/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import type { TransferDetail, TransferList, TransferRow } from "@/lib/transfers-api"
import { server } from "@/test/msw/server"

import { TransfersTab } from "./TransfersTab"

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))

const screenSize = vi.hoisted(() => ({ phone: false }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsPhone: () => screenSize.phone }))

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
const exported = row({ id: 3, kind: "export", name: "Album.2020.FLAC-XYZ", state: "done", finishedAt: finished, startedBy: "rule", ruleId: 9 })
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
    { name: "pass by BitTorrent", at: "2026-11-17T10:00:05Z", detail: "" },
    { name: "teleport", at: "2026-11-17T10:00:06Z", detail: "" },
    { name: "failed", at: "2026-11-17T10:00:07Z", detail: "" },
  ],
  log: [{ at: "2026-11-17T10:00:05Z", message: "Passing 70 B by a Transfer torrent from seedbox to homelab." }],
  members: [{ hash: "bbbb", name: "Film.2019.2160p-ABC", targetInstanceId: 2, savePath: "/srv/movies", category: "", tags: [] }],
}

beforeEach(() => {
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
  cleanup()
  vi.clearAllMocks()
})

function renderTab() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TransfersTab />
    </QueryClientProvider>
  )
}

async function rowOf(name: string): Promise<HTMLElement> {
  const cell = await screen.findByText(name)
  const tr = cell.closest("tr")
  if (!tr) throw new Error(`no row of ${name}`)
  return tr
}

const buttonNames = (tr: HTMLElement) => within(tr).queryAllByRole("button").map(button => button.textContent)

describe("the Transfers tab", () => {
  it("shows each row with what it allows, and lists the rows of a chip, with the count of each chip", async () => {
    renderTab()
    const tr = await rowOf("Show.S01.1080p-GRP")
    const text = tr.textContent ?? ""
    expect(text).toContain("+2 cross-seeds")
    expect(text).toContain("seedbox → homelab")
    expect(text).toContain("Copy over SFTP")
    expect(text).toContain("50%")
    expect(text).toContain("1 MiB/s")
    // 512 MiB left at 1 MiB/s.
    expect(text).toContain("8m")
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
    expect(buttonNames(exportRow)).toEqual(["Details", "Remove"])

    const retryRow = await rowOf("Doc.2018.720p-QRS")
    expect(retryRow.textContent).toMatch(/Retry in \d+ years?/)
    expect(buttonNames(retryRow)).toEqual(["Details", "Retry", "Cancel"])

    const chips = within(screen.getByRole("group", { name: "Show" })).getAllByRole("button").map(chip => chip.textContent)
    expect(chips).toEqual(["All", "Active (1)", "Waiting (0)", "Finished (1)", "Failed (2)"])

    fireEvent.click(screen.getByRole("button", { name: "Failed (2)" }))
    await waitFor(() => expect(screen.queryByText("Show.S01.1080p-GRP")).toBeNull())
    expect(screen.getByText("Film.2019.2160p-ABC")).toBeTruthy()
    expect(requests).toContain("GET /api/transfers?filter=failed")
    expect(screen.getByRole("button", { name: "Failed (2)" }).getAttribute("aria-pressed")).toBe("true")
  })

  it("puts the actions of a row in one menu on a phone", async () => {
    screenSize.phone = true
    renderTab()
    const tr = await rowOf("Film.2019.2160p-ABC")
    const menu = within(tr).getByRole("button", { name: "Actions for Film.2019.2160p-ABC" })
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

  it("opens the Details panel with the reason, torrents, steps, and log", async () => {
    renderTab()
    fireEvent.click(within(await rowOf("Film.2019.2160p-ABC")).getByRole("button", { name: "Details" }))
    const panel = await screen.findByRole("dialog")
    await within(panel).findByText("Transfer of Film.2019.2160p-ABC")
    const text = panel.textContent ?? ""
    expect(text).toContain("Why it failed")
    expect(text).toContain("A file on the target does not match.")
    expect(text).toContain("On homelab, in /srv/movies")
    expect(text).toContain("Started")
    expect(text).toContain("Passing by a Transfer torrent")
    expect(text).toContain("Another step")
    expect(text).not.toContain("teleport")
    expect(text).toContain("Passing 70 B by a Transfer torrent from seedbox to homelab.")
  })
})
