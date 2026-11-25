/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ShareSettingsForm } from "@/components/shares/ShareSettingsForm"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { ShareSettings } from "@/lib/shares-api"
import { server } from "@/test/msw/server"

const defaults: ShareSettings = {
  port: 0,
  publicAddress: "",
  trustedProxies: [],
  trackerUrl: "",
  maxActive: 10,
  peersPerShare: 20,
  defaults: { days: 7, endAtFirstDownload: true, firstDownloaderOnly: true },
  trackerInUse: "",
}

let stored: ShareSettings
let saved: ShareSettings[]
let refuseWith: { status: number; error: string } | null

function renderForm() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ShareSettingsForm />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

function field(label: string): HTMLInputElement {
  return screen.getByLabelText(label) as HTMLInputElement
}

function type(label: string, value: string) {
  fireEvent.change(field(label), { target: { value } })
}

beforeEach(() => {
  stored = structuredClone(defaults)
  saved = []
  refuseWith = null
  server.use(
    http.get("*/api/shares/settings", () => HttpResponse.json(stored)),
    http.put("*/api/shares/settings", async ({ request }) => {
      const body = (await request.json()) as ShareSettings
      saved.push(body)
      if (refuseWith) {
        return HttpResponse.json({ error: refuseWith.error }, { status: refuseWith.status })
      }
      if (!Number.isInteger(body.port) || body.port < 0 || body.port > 65535) {
        return HttpResponse.json({ error: "invalid Share tracker settings: the port is not between 0 and 65535" }, { status: 400 })
      }
      const own = body.port !== 0 && body.publicAddress !== ""
      stored = { ...body, trackerInUse: own ? "own" : body.trackerUrl ? "outside" : "" }
      return HttpResponse.json(stored)
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("ShareSettingsForm", () => {
  it("turns on qui's own tracker with a port, a public address, and trusted proxies", async () => {
    renderForm()
    await screen.findByLabelText("Port")

    type("Port", "6969")
    type("Public address", "https://share.example.com")
    type("Trusted proxies", "172.17.0.1\n\n10.0.0.0/8 ")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toMatchObject({
      port: 6969,
      publicAddress: "https://share.example.com",
      trustedProxies: ["172.17.0.1", "10.0.0.0/8"],
      trackerUrl: "",
    })
    expect(await screen.findByText("New Shares use qui's tracker.")).not.toBeNull()
  })

  it("switches to an outside tracker and closes qui's port", async () => {
    stored = { ...defaults, port: 6969, publicAddress: "https://share.example.com", trackerInUse: "own" }
    renderForm()
    fireEvent.click(await screen.findByRole("radio", { name: "An outside tracker" }))

    type("Tracker URL", "udp://tracker.example.org:1337/announce")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toMatchObject({ port: 0, publicAddress: "", trackerUrl: "udp://tracker.example.org:1337/announce" })
    expect(await screen.findByText("New Shares use the outside tracker.")).not.toBeNull()
  })

  it("says that a Share needs a tracker while none is set, and saves the limits and the defaults for a new Share", async () => {
    renderForm()

    expect(await screen.findByText("No tracker is set, so no Share can start.")).not.toBeNull()
    expect(field("Active Shares").value).toBe("10")
    expect(field("Peers for each Share").value).toBe("20")
    expect(field("Days until a Share ends").value).toBe("7")
    expect(screen.getByRole("switch", { name: "End at the first complete download" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("switch", { name: "Only the first downloader" }).getAttribute("aria-checked")).toBe("true")

    type("Active Shares", "3")
    type("Peers for each Share", "5")
    type("Days until a Share ends", "2")
    fireEvent.click(screen.getByRole("switch", { name: "End at the first complete download" }))
    fireEvent.click(screen.getByRole("switch", { name: "Only the first downloader" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toMatchObject({
      maxActive: 3,
      peersPerShare: 5,
      defaults: { days: 2, endAtFirstDownload: false, firstDownloaderOnly: false },
    })
  })

  it("refuses a port that is not a whole number, and keeps qui's tracker on", async () => {
    stored = { ...defaults, port: 6969, publicAddress: "https://share.example.com", trackerInUse: "own" }
    renderForm()
    await screen.findByLabelText("Port")

    type("Port", "6969.5")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("the port is not between 0 and 65535")
    expect(stored.port).toBe(6969)
    expect(screen.getByText("New Shares use qui's tracker.")).not.toBeNull()
  })

  it("shows why qui refused to move its tracker while Shares on it run", async () => {
    stored = { ...defaults, port: 6969, publicAddress: "https://share.example.com", trackerInUse: "own" }
    refuseWith = { status: 409, error: "End the active Shares on qui's tracker first: their .torrent files carry the old address." }
    renderForm()
    await screen.findByLabelText("Port")

    type("Port", "7000")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("End the active Shares on qui's tracker first: their .torrent files carry the old address.")
    expect(field("Port").value).toBe("7000")
  })
})
