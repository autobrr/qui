/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it } from "vitest"

import { crossSeedRoutesKey } from "@/hooks/useCrossSeedRoutes"
import "@/i18n"
import { server } from "@/test/msw/server"

import { CrossSeedDeleteNotice, RouteProtectionConfirmDialog, useCrossSeedDeleteCheck } from "../RouteProtectionConfirm"

const instances = {
  instances: [
    { id: 1, name: "library-a", hardlinkBaseDir: "/data/a" },
    { id: 2, name: "library-b", hardlinkBaseDir: "/data/b" },
    { id: 3, name: "seedbox", hardlinkBaseDir: "" },
    { id: 4, name: "other", hardlinkBaseDir: "" },
  ],
}

afterEach(cleanup)

// A delete dialog in small: the notice, and a Delete button that waits for it.
function DeleteHarness({ instanceId, enabled = true }: { instanceId: number; enabled?: boolean }) {
  const check = useCrossSeedDeleteCheck(instanceId, enabled)
  return (
    <>
      <CrossSeedDeleteNotice check={check} />
      <button disabled={check.pending}>Delete</button>
    </>
  )
}

function renderNotice(instanceId: number, seed?: (queryClient: QueryClient) => void) {
  // The same 5-second staleTime as App.tsx, so a fresh cached read must not hide a change.
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 5000 } } })
  server.use(http.get("*/api/instances", () => HttpResponse.json(instances.instances)))
  seed?.(queryClient)
  const view = render(
    <QueryClientProvider client={queryClient}>
      <DeleteHarness instanceId={instanceId} />
    </QueryClientProvider>
  )
  return { ...view, queryClient, deleteButton: () => screen.getByRole<HTMLButtonElement>("button", { name: "Delete" }) }
}

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
    renderNotice(3)

    await screen.findByText("3 routed torrents on seedbox use files in the affected base folders of library-a.")
    await screen.findByText("5 routed torrents on seedbox use files in the affected base folders of library-b.")
    const text = document.body.textContent ?? ""
    expect(text).toContain("library-a adds cross-seeds to itself again after this delete.")
    expect(text).toContain("library-b adds cross-seeds to itself again after this delete.")
    expect(text).not.toContain("other")
  })

  it("renders nothing for an instance that no Source instance routes to", async () => {
    server.use(http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 4, crossSeedInstanceId: 1, rows: [] }])))
    const { container, queryClient, deleteButton } = renderNotice(3)
    await waitFor(() => expect(queryClient.getQueryState(crossSeedRoutesKey)?.status).toBe("success"))
    expect(container.textContent).toBe("Delete")
    expect(deleteButton().disabled).toBe(false)
  })

  it("keeps Delete off until each route read ends, and counts a failed read as unknown", async () => {
    let answerList: (() => void) | undefined
    server.use(
      http.get("*/api/cross-seed/routes", () => new Promise<Response>(resolve => {
        answerList = () => resolve(HttpResponse.json([{ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] }]))
      })),
      http.get("*/api/cross-seed/routes/1", () => HttpResponse.json({ error: "boom" }, { status: 500 }))
    )
    const { deleteButton } = renderNotice(3)
    expect(deleteButton().disabled).toBe(true)

    await waitFor(() => expect(answerList).toBeDefined())
    answerList?.()
    await screen.findByText("seedbox is unavailable, so the number of routed torrents in the affected base folders of library-a is unknown.")
    expect(deleteButton().disabled).toBe(false)
  })

  it("says so and lets the delete go on when the route list fails", async () => {
    server.use(http.get("*/api/cross-seed/routes", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
    const { deleteButton } = renderNotice(3)

    await screen.findByText("qui could not read the cross-seed routes, so it cannot name the Source instances that lose protection after this delete.")
    expect(deleteButton().disabled).toBe(false)
  })

  it("says so when a refetch of an old cached route list fails", async () => {
    server.use(http.get("*/api/cross-seed/routes", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
    const { deleteButton } = renderNotice(3, queryClient => {
      queryClient.setQueryData(crossSeedRoutesKey, [{ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] }], { updatedAt: 0 })
    })

    await screen.findByText("qui could not read the cross-seed routes, so it cannot name the Source instances that lose protection after this delete.")
    expect(deleteButton().disabled).toBe(false)
  })

  it("reads the route list again for each dialog, also when the cached list is fresh", async () => {
    // Another browser can have saved a route to the instance after the last list read.
    server.use(
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] }])),
      http.get("*/api/cross-seed/routes/1", () => HttpResponse.json({ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [{ baseDir: "/data/a", mappedPath: "", routedTorrents: 2 }] }))
    )
    const { deleteButton } = renderNotice(3, queryClient => queryClient.setQueryData(crossSeedRoutesKey, []))
    expect(deleteButton().disabled).toBe(true)

    await screen.findByText("2 routed torrents on seedbox use files in the affected base folders of library-a.")
    expect(deleteButton().disabled).toBe(false)
  })

  it("reads the instance list again for each dialog, so it names a Source instance that another tab created", async () => {
    let known = instances.instances.filter(instance => instance.id !== 1)
    server.use(
      http.get("*/api/instances", () => HttpResponse.json(known)),
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] }])),
      http.get("*/api/cross-seed/routes/1", () => HttpResponse.json({ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [{ baseDir: "/data/a", mappedPath: "", routedTorrents: 2 }] }))
    )
    // The app keeps a read fresh for 5 seconds, so the dialog must read the list again anyway.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 5000 } } })
    // The instance card read the instance list before another tab created library-a.
    queryClient.setQueryData(["instances"], known)
    const view = render(<QueryClientProvider client={queryClient}><DeleteHarness instanceId={3} enabled={false} /></QueryClientProvider>)
    await waitFor(() => expect(queryClient.getQueryState(["instances"])?.fetchStatus).toBe("idle"))

    known = instances.instances
    view.rerender(<QueryClientProvider client={queryClient}><DeleteHarness instanceId={3} /></QueryClientProvider>)
    await screen.findByText("2 routed torrents on seedbox use files in the affected base folders of library-a.")
  })

  it("keeps Delete off and shows no old count while a cached route read refetches", async () => {
    let answerRead: (() => void) | undefined
    server.use(
      http.get("*/api/cross-seed/routes", () => HttpResponse.json([{ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [] }])),
      http.get("*/api/cross-seed/routes/1", () => new Promise<Response>(resolve => {
        answerRead = () => resolve(HttpResponse.json({ sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [{ baseDir: "/data/a", mappedPath: "", routedTorrents: 4 }] }))
      }))
    )
    const { deleteButton } = renderNotice(3, queryClient => {
      queryClient.setQueryData([...crossSeedRoutesKey, 1, "/data/a"], { sourceInstanceId: 1, crossSeedInstanceId: 3, rows: [{ baseDir: "/data/a", mappedPath: "", routedTorrents: 9 }] })
    })

    await waitFor(() => expect(answerRead).toBeDefined())
    expect(document.body.textContent).not.toContain("9 routed torrents")
    expect(deleteButton().disabled).toBe(true)
    answerRead?.()
    await screen.findByText("4 routed torrents on seedbox use files in the affected base folders of library-a.")
    expect(deleteButton().disabled).toBe(false)
  })
})
