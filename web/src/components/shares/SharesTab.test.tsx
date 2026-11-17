/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { SharesTab } from "@/components/shares/SharesTab"
import "@/i18n"
import type { Share } from "@/lib/shares-api"
import { server } from "@/test/msw/server"

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
      <SharesTab />
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
  const row = title.closest("li")
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

afterEach(cleanup)

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
    shares = [{ ...active, state: "ended", endReason: "The recipient has the files.", endedAt: "2026-11-18T09:00:00Z" }]
    renderTab()

    const row = await findRow(active.name)
    expect(row.textContent).toContain("Ended")
    expect(row.textContent).toContain("The recipient has the files.")
    expect(screen.getByText("Ended Shares stay in the list for 30 days.")).not.toBeNull()
    expect(within(row).queryByRole("button", { name: "Stop" })).toBeNull()

    fireEvent.click(within(row).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(writes).toEqual(["DELETE /api/shares/41"]))
  })
})
