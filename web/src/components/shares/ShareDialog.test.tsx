/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ShareDialog } from "@/components/shares/ShareDialog"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { Share, ShareSettings, StartSharesRequest } from "@/lib/shares-api"
import { server } from "@/test/msw/server"

const clipboard = vi.hoisted(() => ({ copied: [] as string[] }))
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  copyTextToClipboard: async (text: string) => {
    clipboard.copied.push(text)
  },
}))

const ownTracker: ShareSettings = {
  port: 6969,
  publicAddress: "https://share.example.com",
  trustedProxies: [],
  trackerUrl: "",
  maxActive: 10,
  peersPerShare: 20,
  defaults: { days: 7, endAtFirstDownload: true, firstDownloaderOnly: true },
  trackerInUse: "own",
}

const torrent = { hash: "aaaa1111", name: "Example.Movie.2024.1080p.WEB-DL-GRP" }

function startedShare(request: StartSharesRequest): Share {
  return {
    id: 41,
    instanceId: request.instanceId,
    hash: request.hashes[0],
    name: torrent.name,
    size: 3 * 1024 ** 3,
    tracker: "own",
    endAtFirstDownload: request.endAtFirstDownload,
    firstDownloaderOnly: request.firstDownloaderOnly,
    state: "active",
    endReason: "",
    createdAt: "2026-11-17T10:00:00Z",
    expiresAt: "2026-11-24T10:00:00Z",
    endedAt: null,
    peers: [],
    progress: 0,
    downloaded: 0,
    uploaded: 0,
  }
}

let settings: ShareSettings
let started: StartSharesRequest[]
let refusal: { error: string; refused: Array<{ hash: string; reason: string }> } | null

function renderDialog(torrents = [torrent]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ShareDialog open onOpenChange={vi.fn()} instanceId={3} torrents={torrents} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  settings = structuredClone(ownTracker)
  started = []
  refusal = null
  clipboard.copied = []
  server.use(
    http.get("*/api/shares/settings", () => HttpResponse.json(settings)),
    http.post("*/api/shares", async ({ request }) => {
      const body = (await request.json()) as StartSharesRequest
      started.push(body)
      if (refusal) {
        return HttpResponse.json({ ...refusal, shares: [] }, { status: 409 })
      }
      return HttpResponse.json({ shares: [startedShare(body)], refused: [] }, { status: 201 })
    })
  )
})

afterEach(cleanup)

describe("ShareDialog", () => {
  it("fills the fields from the defaults, names the tracker, creates the Share, and shows the .torrent download and the message to copy", async () => {
    renderDialog()
    const days = (await screen.findByLabelText("Days until a Share ends")) as HTMLInputElement
    expect(days.value).toBe("7")
    expect(screen.getByRole("switch", { name: "End at the first complete download" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("switch", { name: "Only the first downloader" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("dialog").textContent).toContain("qui's tracker at https://share.example.com")

    fireEvent.change(days, { target: { value: "2" } })
    fireEvent.click(screen.getByRole("switch", { name: "Only the first downloader" }))
    fireEvent.click(screen.getByRole("button", { name: "Create" }))

    const download = await screen.findByRole("link", { name: ".torrent" })
    expect(started).toEqual([{ instanceId: 3, hashes: ["aaaa1111"], days: 2, endAtFirstDownload: true, firstDownloaderOnly: false }])
    expect(download.getAttribute("href")).toBe("/api/shares/41/torrent")
    expect(download.hasAttribute("download")).toBe(true)
    expect(screen.queryByRole("button", { name: "Create" })).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "Copy message" }))

    await waitFor(() => expect(clipboard.copied).toHaveLength(1))
    const message = clipboard.copied[0]
    expect(message).toContain("Example.Movie.2024.1080p.WEB-DL-GRP")
    expect(message).toContain("qBittorrent, Transmission, or Deluge")
    expect(message).toContain("2026")
    expect(message).toContain("one download completes")
  })

  it("leaves out the first-download end from the message when the Share does not end there", async () => {
    renderDialog()
    fireEvent.click(await screen.findByRole("switch", { name: "End at the first complete download" }))
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.click(await screen.findByRole("button", { name: "Copy message" }))

    await waitFor(() => expect(clipboard.copied).toHaveLength(1))
    expect(clipboard.copied[0]).not.toContain("one download completes")
  })

  it("says that the lock is not available with an outside tracker, and sends it off", async () => {
    settings = { ...ownTracker, port: 0, publicAddress: "", trackerUrl: "udp://tracker.example.org:1337/announce", trackerInUse: "outside" }
    renderDialog()

    const lock = await screen.findByRole("switch", { name: "Only the first downloader" })
    expect(lock.getAttribute("aria-checked")).toBe("false")
    expect(lock.hasAttribute("disabled")).toBe(true)
    const dialog = screen.getByRole("dialog")
    expect(dialog.textContent).toContain("Outside tracker udp://tracker.example.org:1337/announce")
    expect(dialog.textContent).toContain("Not available with an outside tracker")

    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    await waitFor(() => expect(started).toHaveLength(1))
    expect(started[0].firstDownloaderOnly).toBe(false)
  })

  it("starts nothing while no tracker is set", async () => {
    settings = { ...ownTracker, port: 0, publicAddress: "", trackerInUse: "" }
    renderDialog()

    expect(await screen.findByText("No tracker is set, so no Share can start.")).not.toBeNull()
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Create" }).disabled).toBe(true)
  })

  it("shows the reason when qui refuses the Share, and stays on the form", async () => {
    refusal = {
      error: "Share needs qBittorrent 5.1 or newer. Update qBittorrent on Seedbox.",
      refused: [{ hash: "aaaa1111", reason: "Share needs qBittorrent 5.1 or newer. Update qBittorrent on Seedbox." }],
    }
    renderDialog()
    fireEvent.click(await screen.findByRole("button", { name: "Create" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("Share needs qBittorrent 5.1 or newer. Update qBittorrent on Seedbox.")
    expect(screen.getByRole("button", { name: "Create" })).not.toBeNull()
  })

  // regression: #4242
  it("shows the error of a request that qui cannot read, and stays on the form", async () => {
    server.use(http.post("*/api/shares", () => HttpResponse.json({ error: "Invalid request body" }, { status: 400 })))
    renderDialog()
    fireEvent.change(await screen.findByLabelText("Days until a Share ends"), { target: { value: "1.5" } })
    fireEvent.click(screen.getByRole("button", { name: "Create" }))

    expect((await screen.findByRole("alert")).textContent).toContain("Invalid request body")
    expect(screen.getByRole("button", { name: "Create" })).not.toBeNull()
  })

  it("makes one Share for each torrent and names the torrents that qui refused", async () => {
    const other = { hash: "bbbb2222", name: "Example.Show.S01.1080p.WEB-DL-GRP" }
    server.use(
      http.post("*/api/shares", async ({ request }) => {
        const body = (await request.json()) as StartSharesRequest
        started.push(body)
        return HttpResponse.json(
          { shares: [startedShare(body)], refused: [{ hash: other.hash, reason: "Torrents with skipped files are unsupported" }] },
          { status: 201 }
        )
      })
    )
    renderDialog([torrent, other])
    fireEvent.click(await screen.findByRole("button", { name: "Create" }))

    await screen.findByRole("link", { name: ".torrent" })
    expect(started[0].hashes).toEqual(["aaaa1111", "bbbb2222"])
    const refused = screen.getByRole("list", { name: "Not shared" })
    expect(within(refused).getByText(/Example\.Show\.S01\.1080p\.WEB-DL-GRP/).textContent).toContain("Torrents with skipped files are unsupported")
  })
})
