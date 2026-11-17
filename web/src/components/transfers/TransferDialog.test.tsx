/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { Profiler } from "react"
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { getLinuxIsoName } from "@/lib/incognito"
import type { TransferRequest, TransferSummary } from "@/lib/transfers-api"
import { server } from "@/test/msw/server"

import { TransferDialog } from "./TransferDialog"

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))

const incognito = vi.hoisted(() => ({ on: false }))
vi.mock("@/lib/incognito", async importOriginal => {
  const off: [boolean, (value: boolean) => void] = [false, () => {}]
  const on: [boolean, (value: boolean) => void] = [true, () => {}]
  return { ...await importOriginal<typeof import("@/lib/incognito")>(), useIncognitoMode: () => incognito.on ? on : off }
})

const GiB = 1024 * 1024 * 1024

let summary: TransferSummary
let refusal: string | null
let summaries: TransferRequest[]
let starts: TransferRequest[]
let commits: number

beforeAll(() => {
  // Radix Select asks for pointer capture and scrolls its options into view; jsdom has neither.
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.scrollIntoView = () => {}
})

beforeEach(() => {
  summary = {
    way: "sftp", bytes: 4 * GiB, free: 100 * GiB, freeKnown: true, noFileAccess: false, hints: [],
    members: [
      { hash: "aaaa", name: "Show.S01.1080p-GRP", targetInstanceId: 2, savePath: "/srv/tv", category: "", tags: [] },
      { hash: "bbbb", name: "Show.S01.1080p-GRP.cs", targetInstanceId: 3, savePath: "/srv/cs", category: "", tags: [] },
    ],
  }
  refusal = null
  summaries = []
  starts = []
  commits = 0
  server.use(
    http.get("*/api/instances", () => HttpResponse.json([{ id: 1, name: "seedbox" }, { id: 2, name: "homelab" }, { id: 3, name: "crossbox" }])),
    http.get("*/api/instances/:id/categories", () => HttpResponse.json({ tv: { name: "tv", savePath: "" } })),
    http.get("*/api/instances/:id/tags", () => HttpResponse.json([])),
    http.post("*/api/transfers/summary", async ({ request }) => {
      summaries.push(await request.json() as TransferRequest)
      return refusal ? HttpResponse.json({ error: refusal }, { status: 409 }) : HttpResponse.json(summary)
    }),
    http.post("*/api/transfers", async ({ request }) => {
      starts.push(await request.json() as TransferRequest)
      return HttpResponse.json({ id: 5 }, { status: 202 })
    })
  )
})

afterEach(() => {
  incognito.on = false
  cleanup()
  vi.clearAllMocks()
})

const newQueryClient = () => new QueryClient({ defaultOptions: { queries: { retry: false } } })

function renderDialog(queryClient = newQueryClient()) {
  const onOpenChange = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Profiler id="dialog" onRender={() => { commits++ }}>
          <TransferDialog open onOpenChange={onOpenChange} sourceInstanceId={1} hash="aaaa" name="Show.S01.1080p-GRP" />
        </Profiler>
      </TooltipProvider>
    </QueryClientProvider>
  )
  return onOpenChange
}

async function pickTarget(name: string) {
  const trigger = await screen.findByRole("combobox", { name: "Target instance" })
  fireEvent.keyDown(trigger, { key: "Enter" })
  fireEvent.click(await screen.findByRole("option", { name }))
}

const summaryPanel = () => document.querySelector<HTMLElement>("section[aria-labelledby='transfer-summary-title']")!
const summaryText = () => summaryPanel().textContent ?? ""
const figureLabels = () => Array.from(summaryPanel().querySelectorAll("dt"), label => label.textContent)
/** The value under one label of the figures at the top of the summary. */
const figure = (label: string) =>
  Array.from(summaryPanel().querySelectorAll("dt")).find(term => term.textContent === label)?.nextElementSibling?.textContent ?? null
const memberRow = (name: string) => Array.from(summaryPanel().querySelectorAll("li")).find(row => row.firstElementChild?.textContent === name)?.textContent ?? null
const startButton = () => screen.getByRole<HTMLButtonElement>("button", { name: "Start Transfer" })

describe("the Transfer dialog", () => {
  it("keeps the target picker while the instances load, and asks for another instance only when none exists", async () => {
    let answer: () => void = () => {}
    const loaded = new Promise<void>(resolve => { answer = resolve })
    server.use(http.get("*/api/instances", async () => {
      await loaded
      return HttpResponse.json([{ id: 1, name: "seedbox" }])
    }))
    renderDialog()
    expect(screen.getByRole("combobox", { name: "Target instance" })).toBeTruthy()
    expect(screen.queryByText("Add another instance to transfer torrents to it.")).toBeNull()

    answer()
    await screen.findByText("Add another instance to transfer torrents to it.")
    expect(screen.queryByRole("combobox", { name: "Target instance" })).toBeNull()
  })

  it("offers every instance but the source as the target, shows the summary of the Transfer before it starts, and starts what the summary showed", async () => {
    const onOpenChange = renderDialog()
    const trigger = await screen.findByRole("combobox", { name: "Target instance" })
    expect(summaryText()).toContain("Select a target instance to see the summary.")
    expect(startButton().disabled).toBe(true)
    fireEvent.keyDown(trigger, { key: "Enter" })
    expect((await screen.findAllByRole("option")).map(option => option.textContent)).toEqual(["homelab", "crossbox"])
    expect(summaries).toEqual([])
    fireEvent.click(screen.getByRole("option", { name: "homelab" }))
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "moved,, kept, " } })

    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    expect(figure("Size")).toBe("4 GiB")
    expect(figure("Way")).toBe("Copy over SFTP")
    expect(figure("Free on the target")).toBe("100 GiB")
    expect(memberRow("Show.S01.1080p-GRP")).toBe("Show.S01.1080p-GRP")
    expect(memberRow("Show.S01.1080p-GRP.cs")).toBe("Show.S01.1080p-GRP.csOn crossbox")
    expect(summaryText()).not.toContain("less free space")
    expect(starts).toEqual([])

    await waitFor(() => expect(startButton().disabled).toBe(false))
    fireEvent.click(startButton())
    await waitFor(() => expect(starts).toHaveLength(1))
    expect(starts[0]).toEqual({
      sourceInstanceId: 1, hash: "aaaa", targetInstanceId: 2, savePath: "", category: "", tags: ["moved", "kept"],
      after: "removeWithFiles", leaveStopped: false, way: "auto",
    })
    expect(starts[0]).toEqual(summaries.at(-1))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(toast.success).toHaveBeenCalledWith("The Transfer of Show.S01.1080p-GRP waits for its turn.")
  })

  it("warns about free space, a target without file access, and TCP and μTP", async () => {
    summary = { ...summary, way: "bittorrent", free: 1 * GiB, noFileAccess: true, hints: [{ kind: "tcpAndUtp", instanceId: 2 }] }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(figure("Way")).toBe("BitTorrent"))
    const warnings = within(screen.getByRole("dialog")).getAllByRole("alert").map(alert => alert.textContent)
    expect(warnings).toEqual([
      "The target has less free space than the Transfer needs.",
      "qui cannot see the files on homelab, so a file with other content at the same path can be replaced.",
    ])
    expect(summaryText()).toContain("homelab uses qBittorrent's default \"TCP and μTP\"")
  })

  it("marks a cross-seed of the same target, and leaves the torrent itself unmarked", async () => {
    summary = { ...summary, members: summary.members.map(member => ({ ...member, targetInstanceId: 2 })) }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    expect(memberRow("Show.S01.1080p-GRP")).toBe("Show.S01.1080p-GRP")
    expect(memberRow("Show.S01.1080p-GRP.cs")).toBe("Show.S01.1080p-GRP.csCross-seed")
  })

  // regression: #4242
  it("keeps the same figures in place before, while, and after a summary loads", async () => {
    let answer: () => void = () => {}
    server.use(http.post("*/api/transfers/summary", async ({ request }) => {
      summaries.push(await request.json() as TransferRequest)
      await new Promise<void>(resolve => { answer = resolve })
      return HttpResponse.json(summary)
    }))
    const labels = ["Torrents", "Size", "Way", "Free on the target"]
    renderDialog()
    await screen.findByRole("combobox", { name: "Target instance" })
    expect(figureLabels()).toEqual(labels)
    expect(figure("Torrents")).toBe("—")

    await pickTarget("homelab")
    await waitFor(() => expect(summaries).toHaveLength(1))
    expect(figureLabels()).toEqual(labels)
    expect(summaryPanel().getAttribute("aria-busy")).toBe("true")
    act(() => answer())
    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    expect(summaryPanel().getAttribute("aria-busy")).toBe("false")

    // The next summary loads behind the last one, and Start waits for it.
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "kept" } })
    await waitFor(() => expect(summaries).toHaveLength(2))
    expect(figureLabels()).toEqual(labels)
    expect(figure("Torrents")).toBe("2")
    expect(summaryPanel().getAttribute("aria-busy")).toBe("true")
    expect(startButton().disabled).toBe(true)
    act(() => answer())
    await waitFor(() => expect(startButton().disabled).toBe(false))
    expect(summaryPanel().getAttribute("aria-busy")).toBe("false")
  })

  it("says when qBittorrent does not report the free space", async () => {
    summary = { ...summary, freeKnown: false, free: 0 }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(summaryText()).toContain("Free space unknown"))
    expect(figure("Free on the target")).toBe("Unknown")
    expect(summaryText()).not.toContain("less free space")
  })

  it("shows why qui refuses the Transfer, and does not start it", async () => {
    refusal = "The torrent is already moving to homelab."
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(summaryText()).toContain("qui does not start this Transfer: The torrent is already moving to homelab."))
    expect(startButton().disabled).toBe(true)
  })

  // regression: #4242
  it("starts the Transfer into a target category whose name equals the value of No category", async () => {
    server.use(http.get("*/api/instances/:id/categories", () => HttpResponse.json({ __none__: { name: "__none__", savePath: "" } })))
    renderDialog()
    await pickTarget("homelab")
    const category = screen.getByRole("combobox", { name: "Category" })
    fireEvent.keyDown(category, { key: "Enter" })
    fireEvent.click(await screen.findByRole("option", { name: "__none__" }))

    await waitFor(() => expect(startButton().disabled).toBe(false))
    fireEvent.click(startButton())
    await waitFor(() => expect(starts).toHaveLength(1))
    expect(starts[0].category).toBe("__none__")
  })

  // regression: #4242
  it("renders nothing more while the form stays as it is", async () => {
    // The categories and tags of the target are in the cache, so no load of them ends during the wait.
    const queryClient = newQueryClient()
    queryClient.setQueryData(["instance-metadata", 2], { categories: { tv: { name: "tv", savePath: "" } }, tags: [] })
    renderDialog(queryClient)
    await pickTarget("homelab")
    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    const settled = commits
    await act(() => new Promise(resolve => setTimeout(resolve, 500)))
    expect(commits).toBe(settled)
  })

  // regression: #4242
  it("shows made-up names in incognito mode, and hides the reason of a refusal", async () => {
    incognito.on = true
    const onOpenChange = renderDialog()
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain(getLinuxIsoName("aaaa"))
    await pickTarget("homelab")
    await waitFor(() => expect(summaryText()).toContain("Copy over SFTP"))
    expect(summaryText()).toContain(getLinuxIsoName("bbbb"))
    expect(dialog.textContent).not.toContain("Show.S01")

    fireEvent.click(startButton())
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(toast.success.mock.calls[0][0]).toContain(getLinuxIsoName("aaaa"))

    refusal = "Only complete torrents can move, and Show.S01.1080p-GRP is not complete."
    await pickTarget("crossbox")
    await waitFor(() => expect(summaryText()).toContain("Hidden in incognito mode"))
    expect(summaryText()).not.toContain("Show.S01")
  })
})
