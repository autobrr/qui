/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider, onlineManager } from "@tanstack/react-query"
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
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.scrollIntoView = () => {}
})

beforeEach(() => {
  summary = {
    way: "sftp", bytes: 4 * GiB, free: 100 * GiB, freeKnown: true, noFileAccess: false, stayOnSource: 0, hints: [],
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

const onStarted = vi.fn()

function renderDialog(queryClient = newQueryClient()) {
  const onOpenChange = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Profiler id="dialog" onRender={() => { commits++ }}>
          <TransferDialog open onOpenChange={onOpenChange} onStarted={onStarted} sourceInstanceId={1} hash="aaaa" name="Show.S01.1080p-GRP" />
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
    expect(onStarted).toHaveBeenCalledOnce()
    expect(toast.success).toHaveBeenCalledWith("The Transfer of Show.S01.1080p-GRP waits for its turn.")
  })

  it("warns about free space and a target without file access next to Start, and hints at TCP and μTP", async () => {
    summary = { ...summary, way: "bittorrent", free: 1 * GiB, noFileAccess: true, stayOnSource: 2, hints: [{ kind: "tcpAndUtp", instanceId: 2 }] }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(figure("Way")).toBe("BitTorrent"))
    const alerts = within(screen.getByRole("dialog")).getAllByRole("alert")
    expect(alerts.map(alert => alert.textContent)).toEqual([
      "2 cross-seeds stay on seedbox. They read these files through hardlinks, and qui cannot make hardlinks on homelab without file access. " +
        "To move them too, give qui file access to homelab, on qui's own disk or over SSH, or cross-seed again on homelab.",
      "The target has less free space than the Transfer needs.",
      "qui cannot see the files on homelab, so a file with other content at the same path can be replaced.",
    ])
    for (const alert of alerts) {
      expect(summaryPanel().compareDocumentPosition(alert) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      expect(alert.compareDocumentPosition(startButton()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    expect(screen.queryByText("qui checks every piece on the target before it changes the source.")).toBeNull()
    expect(summaryText()).toContain("homelab uses qBittorrent's default \"TCP and μTP\"")
  })

  it("names the other ways when a forced Transfer torrent leaves a cross-seed on the source", async () => {
    summary = { ...summary, way: "bittorrent", stayOnSource: 1 }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(figure("Way")).toBe("BitTorrent"))
    expect(within(screen.getByRole("dialog")).getAllByRole("alert").map(alert => alert.textContent)).toEqual([
      "1 cross-seed stays on seedbox. It reads these files through hardlinks, and a Transfer torrent cannot make hardlinks. " +
        "To move it too, select the way \"Auto\" or \"Copy through qui\".",
    ])
  })

  it("marks a cross-seed of the same target, and leaves the torrent itself unmarked", async () => {
    summary = { ...summary, members: summary.members.map(member => ({ ...member, targetInstanceId: 2 })) }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    expect(memberRow("Show.S01.1080p-GRP")).toBe("Show.S01.1080p-GRP")
    expect(memberRow("Show.S01.1080p-GRP.cs")).toBe("Show.S01.1080p-GRP.csCross-seed")
  })

  it("names the target of the summary that shows while the summary of the next target loads", async () => {
    summary = { ...summary, noFileAccess: true }
    renderDialog()
    await pickTarget("homelab")
    await waitFor(() => expect(memberRow("Show.S01.1080p-GRP.cs")).toBe("Show.S01.1080p-GRP.csOn crossbox"))
    let answer: () => void = () => {}
    server.use(http.post("*/api/transfers/summary", async ({ request }) => {
      summaries.push(await request.json() as TransferRequest)
      await new Promise<void>(resolve => { answer = resolve })
      return HttpResponse.json(summary)
    }))

    await pickTarget("crossbox")
    await waitFor(() => expect(summaries.at(-1)?.targetInstanceId).toBe(3))
    expect(memberRow("Show.S01.1080p-GRP")).toBe("Show.S01.1080p-GRP")
    expect(memberRow("Show.S01.1080p-GRP.cs")).toBe("Show.S01.1080p-GRP.csOn crossbox")
    expect(screen.getByText(/qui cannot see the files on homelab/)).toBeTruthy()
    act(() => answer())
    await waitFor(() => expect(screen.getByText(/qui cannot see the files on crossbox/)).toBeTruthy())
  })

  it("shows the figures next to Start on a narrow screen, and lists the torrents on a tap", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: query === "(max-width: 1023px)", media: query, addEventListener: () => {}, removeEventListener: () => {},
    }))
    try {
      let answer: () => void = () => {}
      server.use(http.post("*/api/transfers/summary", async ({ request }) => {
        summaries.push(await request.json() as TransferRequest)
        await new Promise<void>(resolve => { answer = resolve })
        return HttpResponse.json(summary)
      }))
      renderDialog()
      const target = await screen.findByRole("combobox", { name: "Target instance" })
      expect(document.querySelector("section[aria-labelledby='transfer-summary-title']")).toBeNull()
      const hint = screen.getByText("Select a target instance to see the summary.")
      expect(target.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      expect(hint.compareDocumentPosition(startButton()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

      await pickTarget("homelab")
      await waitFor(() => expect(summaries).toHaveLength(1))
      const line = screen.getByRole("region", { name: "Summary" })
      const labels = () => Array.from(line.querySelectorAll("dt"), label => label.textContent)
      const value = (label: string) =>
        Array.from(line.querySelectorAll("dt")).find(term => term.textContent === label)?.nextElementSibling?.textContent
      expect(labels()).toEqual(["Size", "Free on the target", "Way"])
      expect(line.getAttribute("aria-busy")).toBe("true")
      act(() => answer())
      await waitFor(() => expect(value("Size")).toBe("4 GiB"))
      expect(labels()).toEqual(["Size", "Free on the target", "Way"])
      expect(line.compareDocumentPosition(startButton()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

      expect(screen.queryByText("Show.S01.1080p-GRP.cs")).toBeNull()
      const torrents = screen.getByRole("button", { name: /^Torrents/ })
      expect(torrents.textContent).toBe("Torrents2")
      expect(torrents.getAttribute("aria-expanded")).toBe("false")
      fireEvent.click(torrents)
      expect(torrents.getAttribute("aria-expanded")).toBe("true")
      expect(screen.getByText("Show.S01.1080p-GRP.cs").parentElement?.textContent).toBe("Show.S01.1080p-GRP.csOn crossbox")
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it("shows one line of each warning on a phone until a tap opens them, and shows them in full on a wide screen", async () => {
    summary = { ...summary, noFileAccess: true }
    const text = "qui cannot see the files on homelab, so a file with other content at the same path can be replaced."
    renderDialog()
    await pickTarget("homelab")
    expect((await screen.findByText(text)).className).not.toContain("truncate")
    expect(screen.queryByRole("button", { name: "Show the full warnings" })).toBeNull()
    cleanup()

    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: query.startsWith("(max-width:"), media: query, addEventListener: () => {}, removeEventListener: () => {},
    }))
    try {
      renderDialog()
      await pickTarget("homelab")
      const warning = await screen.findByText(text)
      const toggle = screen.getByRole("button", { name: "Show the full warnings" })
      expect(toggle.getAttribute("aria-expanded")).toBe("false")
      expect(warning.className).toContain("truncate")
      fireEvent.click(toggle)
      expect(screen.getByRole("button", { name: "Show one line of each warning" }).getAttribute("aria-expanded")).toBe("true")
      expect(warning.className).not.toContain("truncate")
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it("keeps the same figures in place before, while, and after a summary loads", async () => {
    let answer: () => void = () => {}
    server.use(http.post("*/api/transfers/summary", async ({ request }) => {
      summaries.push(await request.json() as TransferRequest)
      await new Promise<void>(resolve => { answer = resolve })
      return HttpResponse.json(summary)
    }))
    const labels = ["Torrents", "Way", "Size", "Free on the target"]
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

  it("keeps Start off while the browser is offline and the summary of the last form shows", async () => {
    const queryClient = newQueryClient()
    renderDialog(queryClient)
    await pickTarget("homelab")
    await waitFor(() => expect(startButton().disabled).toBe(false))

    onlineManager.setOnline(false)
    try {
      fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "kept" } })
      await waitFor(() => expect(queryClient.getQueryCache().findAll({ queryKey: ["transfers", "summary"] })
        .some(query => query.state.fetchStatus === "paused")).toBe(true))
      expect(figure("Torrents")).toBe("2")
      expect(startButton().disabled).toBe(true)
    } finally {
      act(() => onlineManager.setOnline(true))
    }
    await waitFor(() => expect(startButton().disabled).toBe(false))
    expect(summaries.at(-1)?.tags).toEqual(["kept"])
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

  it("renders nothing more while the form stays as it is", async () => {
    const queryClient = newQueryClient()
    queryClient.setQueryData(["instance-metadata", 2], { categories: { tv: { name: "tv", savePath: "" } }, tags: [] })
    renderDialog(queryClient)
    await pickTarget("homelab")
    await waitFor(() => expect(figure("Torrents")).toBe("2"))
    const settled = commits
    await act(() => new Promise(resolve => setTimeout(resolve, 500)))
    expect(commits).toBe(settled)
  })

  it("leaves the next dialog open when the answer of a start comes after the user closed this one", async () => {
    let answer = () => {}
    const held = new Promise<void>(resolve => { answer = resolve })
    server.use(http.post("*/api/transfers", async ({ request }) => {
      starts.push(await request.json() as TransferRequest)
      await held
      return HttpResponse.json({ id: 5 }, { status: 202 })
    }))
    const onOpenChange = renderDialog()
    await pickTarget("homelab")
    await waitFor(() => expect(startButton().disabled).toBe(false))
    fireEvent.click(startButton())
    await waitFor(() => expect(starts).toHaveLength(1))

    cleanup()
    answer()
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("The Transfer of Show.S01.1080p-GRP waits for its turn."))
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(onStarted).not.toHaveBeenCalled()
  })

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
