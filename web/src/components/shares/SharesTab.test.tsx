/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { SharesTab } from "@/components/shares/SharesTab"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { getLinuxIsoName } from "@/lib/incognito"
import type { Share } from "@/lib/shares-api"
import { server } from "@/test/msw/server"

const screenSize = vi.hoisted(() => ({ phone: false }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsPhone: () => screenSize.phone }))

const incognito = vi.hoisted(() => ({ on: false }))
vi.mock("@/lib/incognito", async importOriginal => {
  const off: [boolean, (value: boolean) => void] = [false, () => {}]
  const on: [boolean, (value: boolean) => void] = [true, () => {}]
  return { ...await importOriginal<typeof import("@/lib/incognito")>(), useIncognitoMode: () => incognito.on ? on : off }
})

const active: Share = {
  id: 41,
  instanceId: 3,
  hash: "aaaa1111",
  name: "Example.Movie.2024.1080p.WEB-DL-GRP",
  size: 4 * 1024 ** 3,
  tracker: "own",
  endAtFirstDownload: true,
  firstDownloaderOnly: true,
  state: "active",
  endReason: "",
  endReasonCode: "",
  createdAt: "2026-11-17T10:00:00Z",
  expiresAt: "2026-11-24T10:00:00Z",
  endedAt: null,
  peers: [{ address: "198.51.100.7", progress: 0.25, seenAt: "2026-11-17T10:05:00Z" }],
  progress: 0.25,
  downloaded: 1024 ** 3,
  uploaded: 1024 ** 3,
}

let shares: Share[]
let writes: string[]

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <SharesTab />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

function record(request: Request) {
  const url = new URL(request.url)
  writes.push(`${request.method} ${url.pathname}`)
  return new HttpResponse(null, { status: 204 })
}

async function findRow(name: string): Promise<HTMLElement> {
  const title = await screen.findByText(name)
  const row = title.closest("tr")
  if (!row) throw new Error(`no row for ${name}`)
  return row
}

beforeEach(() => {
  shares = []
  writes = []
  server.use(
    http.get("*/api/shares", () => HttpResponse.json(shares)),
    http.get("*/api/instances", () => HttpResponse.json([{ id: 3, name: "Seedbox" }])),
    http.post("*/api/shares/:id/stop", ({ request }) => record(request)),
    http.post("*/api/shares/:id/unlock", ({ request }) => record(request)),
    http.delete("*/api/shares/:id", ({ request }) => record(request))
  )
})

afterEach(() => {
  screenSize.phone = false
  incognito.on = false
  cleanup()
  vi.unstubAllGlobals()
})

describe("SharesTab", () => {
  it("shows an empty list and sends nothing while no Share exists", async () => {
    renderTab()

    expect(await screen.findByText("No Shares yet.")).not.toBeNull()
    expect(screen.queryAllByRole("button")).toHaveLength(0)
    expect(writes).toEqual([])
  })

  it("shows an active Share with its instance, end, addresses, progress, bytes sent, and state, and unlocks and stops it", async () => {
    shares = [active]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("Seedbox")
    expect(row.textContent).toContain("Active")
    expect(row.textContent).toContain("Ends")
    expect(row.textContent).toContain("Addresses that announced")
    expect(row.textContent).toContain("198.51.100.7")
    expect(row.textContent).toContain("25%")
    expect(row.textContent).toMatch(/Sent\s*1(\.0+)? GiB/)
    expect(within(row).getByRole("link", { name: ".torrent" }).getAttribute("href")).toBe("/api/shares/41/torrent")

    fireEvent.click(within(row).getByRole("button", { name: "Unlock" }))
    await waitFor(() => expect(writes).toEqual(["POST /api/shares/41/unlock"]))
    const stop = within(row).getByRole<HTMLButtonElement>("button", { name: "Stop" })
    await waitFor(() => expect(stop.disabled).toBe(false))
    fireEvent.click(stop)
    await waitFor(() => expect(writes).toEqual(["POST /api/shares/41/unlock", "POST /api/shares/41/stop"]))
  })

  it("puts the actions of a Share in one menu on a phone, with Stop last", async () => {
    screenSize.phone = true
    shares = [active]
    renderTab()

    const row = await findRow(active.name)
    const menu = within(row).getByRole("button", { name: "More actions" })
    expect(within(row).getAllByRole("button")).toEqual([menu])
    expect(within(row).queryByRole("link")).toBeNull()

    fireEvent.pointerDown(menu, { button: 0, ctrlKey: false })
    const items = await screen.findAllByRole("menuitem")
    expect(items.map((item) => item.textContent)).toEqual([".torrent", "Unlock", "Stop"])
    expect(items[0].getAttribute("href")).toBe("/api/shares/41/torrent")
    fireEvent.click(screen.getByRole("menuitem", { name: "Stop" }))
    await waitFor(() => expect(writes).toEqual(["POST /api/shares/41/stop"]))
  })

  it("lists connected peers and offers no Unlock with an outside tracker", async () => {
    shares = [{ ...active, tracker: "outside" }]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("Connected peers")
    expect(within(row).queryByRole("button", { name: "Unlock" })).toBeNull()
    expect(within(row).getByRole("button", { name: "Stop" })).not.toBeNull()
  })

  it("offers no Unlock for a Share that lets every downloader in", async () => {
    shares = [{ ...active, firstDownloaderOnly: false }]
    renderTab()

    const row = await findRow(active.name)
    expect(within(row).queryByRole("button", { name: "Unlock" })).toBeNull()
  })

  it("shows why an ended Share ended, and removes its row before the 30 days", async () => {
    shares = [{ ...active, state: "ended", endReason: "The recipient has the files.", endReasonCode: "downloaded", endedAt: "2026-11-18T09:00:00Z" }]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("Ended")
    expect(row.textContent).toContain("The recipient has the files.")
    expect(screen.getByText("Ended Shares stay in the list for 30 days.")).not.toBeNull()
    expect(within(row).queryByRole("button", { name: "Stop" })).toBeNull()

    fireEvent.click(within(row).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(writes).toEqual(["DELETE /api/shares/41"]))
  })

  // regression: #4242
  it("shows the end reason in the language of the user, by its code", async () => {
    shares = [{ ...active, state: "ended", endReason: "A text that the code replaces", endReasonCode: "expired", endedAt: "2026-11-18T09:00:00Z" }]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("The Share expired.")
    expect(row.textContent).not.toContain("A text that the code replaces")
  })

  // regression: #4242
  it.each(["other", "a_code_of_a_newer_server"])("shows the text of the server for the end reason code %s", async (code) => {
    shares = [{ ...active, state: "ended", endReason: "qBittorrent did not seed the Share torrent.", endReasonCode: code, endedAt: "2026-11-18T09:00:00Z" }]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("qBittorrent did not seed the Share torrent.")
  })

  // regression: #4242
  it("hides an end reason of the server in incognito mode, and shows a translated one", async () => {
    incognito.on = true
    shares = [
      { ...active, state: "ended", endReason: "qBittorrent did not seed the Share torrent.", endReasonCode: "other", endedAt: "2026-11-18T09:00:00Z" },
      { ...active, id: 42, hash: "bbbb2222", state: "ended", endReason: "The Share expired.", endReasonCode: "expired", endedAt: "2026-11-18T09:00:00Z" },
    ]
    renderTab()

    const other = await findRow(getLinuxIsoName("aaaa1111"))
    expect(other.textContent).not.toContain("qBittorrent did not seed")
    expect(other.textContent).toContain("Hidden in incognito mode")
    expect((await findRow(getLinuxIsoName("bbbb2222"))).textContent).toContain("The Share expired.")
  })

  // regression: #4242
  it("shows a made-up name and hides the addresses in incognito mode", async () => {
    incognito.on = true
    shares = [active]
    renderTab()
    const row = await findRow(getLinuxIsoName("aaaa1111"))
    const text = row.textContent ?? ""
    expect(text).not.toContain("Example.Movie.2024.1080p.WEB-DL-GRP")
    expect(text).not.toContain("198.51.100.7")
    expect(text).toContain("192.168.x.x:xxxxx")
  })
})
