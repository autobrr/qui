/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { TransferRequest, TransferSummary } from "@/lib/transfers-api"
import { server } from "@/test/msw/server"

import { TransferDialog } from "./TransferDialog"

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))

const GiB = 1024 * 1024 * 1024

let summary: TransferSummary
let refusal: string | null
let summaries: TransferRequest[]
let starts: TransferRequest[]

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
  cleanup()
  vi.clearAllMocks()
})

function renderDialog() {
  const onOpenChange = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <TransferDialog open onOpenChange={onOpenChange} sourceInstanceId={1} hash="aaaa" name="Show.S01.1080p-GRP" />
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

const summaryText = () => document.querySelector("section[aria-labelledby='transfer-summary-title']")?.textContent ?? ""
const startButton = () => screen.getByRole<HTMLButtonElement>("button", { name: "Start Transfer" })

describe("the Transfer dialog", () => {
  it("offers every instance but the source as the target, and plans nothing before a target", async () => {
    renderDialog()
    const trigger = await screen.findByRole("combobox", { name: "Target instance" })
    expect(summaryText()).toContain("Select a target instance to see the summary.")
    expect(startButton().disabled).toBe(true)

    fireEvent.keyDown(trigger, { key: "Enter" })
    const options = (await screen.findAllByRole("option")).map(option => option.textContent)
    expect(options).toEqual(["homelab", "crossbox"])
    expect(summaries).toEqual([])
  })

  it("shows the summary of the Transfer before it starts, and starts what the summary showed", async () => {
    const onOpenChange = renderDialog()
    await pickTarget("homelab")
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "moved,, kept, " } })

    await waitFor(() => expect(summaryText()).toContain("2 torrents, 4 GiB"))
    const text = summaryText()
    expect(text).toContain("Way: Copy over SFTP")
    expect(text).toContain("Free on the target: 100 GiB")
    expect(text).toContain("Show.S01.1080p-GRP.cs (On crossbox)")
    expect(text).not.toContain("less free space")
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

    await waitFor(() => expect(summaryText()).toContain("Way: BitTorrent"))
    const warnings = within(screen.getByRole("dialog")).getAllByRole("alert").map(alert => alert.textContent)
    expect(warnings).toEqual([
      "The target has less free space than the Transfer needs.",
      "qui cannot see the files on homelab, so a file with other content at the same path can be replaced.",
    ])
    expect(summaryText()).toContain("homelab uses qBittorrent's default \"TCP and μTP\"")
  })

  it("says when qBittorrent does not report the free space", async () => {
    summary = { ...summary, freeKnown: false, free: 0 }
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(summaryText()).toContain("Free space unknown"))
    expect(summaryText()).not.toContain("less free space")
  })

  it("shows why qui refuses the Transfer, and does not start it", async () => {
    refusal = "The torrent is already moving to homelab."
    renderDialog()
    await pickTarget("homelab")

    await waitFor(() => expect(summaryText()).toContain("qui does not start this Transfer: The torrent is already moving to homelab."))
    expect(startButton().disabled).toBe(true)
  })
})
