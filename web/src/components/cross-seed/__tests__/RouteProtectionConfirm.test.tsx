/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { crossSeedRoutesKey } from "@/hooks/useCrossSeedRoutes"
import "@/i18n"
import { server } from "@/test/msw/server"

import { CrossSeedDeleteNotice, RouteProtectionConfirmDialog } from "../RouteProtectionConfirm"

const instances = vi.hoisted(() => ({
  instances: [
    { id: 1, name: "library-a", hardlinkBaseDir: "/data/a" },
    { id: 2, name: "library-b", hardlinkBaseDir: "/data/b" },
    { id: 3, name: "seedbox", hardlinkBaseDir: "" },
    { id: 4, name: "other", hardlinkBaseDir: "" },
  ],
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))

afterEach(cleanup)

function renderDialog(props: Partial<Parameters<typeof RouteProtectionConfirmDialog>[0]> = {}) {
  render(
    <RouteProtectionConfirmDialog
      open
      onOpenChange={() => {}}
      onConfirm={() => {}}
      sourceName="library-a"
      crossSeedName="seedbox"
      routedTorrents={3}
      {...props}
    />
  )
  return document.body.textContent ?? ""
}

describe("RouteProtectionConfirmDialog", () => {
  it("gives the routed torrent count and says the orphan scan stops protecting them", () => {
    const text = renderDialog()
    expect(text).toContain("3 routed torrents on seedbox use files in the affected base folders of library-a.")
    expect(text).toContain("The orphan scan of library-a stops protecting the files of these torrents.")
    expect(text).not.toContain("Move or delete those torrents")
  })

  it("keeps Continue off while the count loads", () => {
    renderDialog({ routedTorrents: undefined })
    expect(document.body.textContent).toContain("Counting routed torrents")
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Continue" }).disabled).toBe(true)
    cleanup()
    renderDialog({ routedTorrents: null })
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Continue" }).disabled).toBe(false)
  })

  it("gives the notice as the dialog description, for screen readers", () => {
    renderDialog()
    const describedBy = screen.getByRole("alertdialog").getAttribute("aria-describedby")
    expect(document.getElementById(describedBy ?? "")?.textContent).toContain("3 routed torrents on seedbox")
  })

  it("says the count is unknown when the Cross-seed instance is unavailable", () => {
    const text = renderDialog({ routedTorrents: null })
    expect(text).toContain("seedbox is unavailable, so the number of routed torrents in the affected base folders of library-a is unknown.")
  })

  it("tells the user to move the routed torrents before a route clear", () => {
    const text = renderDialog({ clear: true })
    expect(text).toContain("seedbox keeps the routed torrents, and their files stay in the base folders of library-a. Move or delete those torrents before you clear the route.")
  })
})

describe("CrossSeedDeleteNotice", () => {
  it("names each Source instance with the routed torrents under its own rows", async () => {
    server.use(
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([
        { sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] },
        { sourceInstanceId: 2, crossSeedInstanceId: 3, rows: [] },
        { sourceInstanceId: 4, crossSeedInstanceId: 1, rows: [] },
      ])),
      http.get("*/api/cross-seed/routes/1", () => HttpResponse.json({
        sourceInstanceId: 1, crossSeedInstanceId: 3,
        rows: [{ baseDir: "/data/a", mappedPath: "", routedTorrents: 2 }, { baseDir: "/data/a2", mappedPath: "", routedTorrents: 1 }],
      })),
      http.get("*/api/cross-seed/routes/2", () => HttpResponse.json({
        sourceInstanceId: 2, crossSeedInstanceId: 3,
        rows: [{ baseDir: "/data/b", mappedPath: "", routedTorrents: 5 }],
      }))
    )
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <CrossSeedDeleteNotice instanceId={3} />
      </QueryClientProvider>
    )

    await screen.findByText("3 routed torrents on seedbox use files in the affected base folders of library-a.")
    await screen.findByText("5 routed torrents on seedbox use files in the affected base folders of library-b.")
    const text = document.body.textContent ?? ""
    expect(text).toContain("library-a adds cross-seeds to itself again after this delete.")
    expect(text).toContain("library-b adds cross-seeds to itself again after this delete.")
    expect(text).not.toContain("other")
  })

  it("renders nothing for an instance that no Source instance routes to", async () => {
    server.use(http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 4, crossSeedInstanceId: 1, rows: [] }])))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <CrossSeedDeleteNotice instanceId={3} />
      </QueryClientProvider>
    )
    await waitFor(() => expect(queryClient.getQueryState(crossSeedRoutesKey)?.status).toBe("success"))
    expect(container.textContent).toBe("")
  })
})
