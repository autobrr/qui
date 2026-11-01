/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider, useSearch } from "@tanstack/react-router"
import { act, cleanup, render, screen } from "@testing-library/react"
import { useCallback, useState } from "react"
import { afterEach, describe, expect, it } from "vitest"

import { useMobileDetailsHistory } from "./useMobileDetailsHistory"

// The page in small: a list with a details view. A torrent in the URL at mount
// opens the view, as the deep link fetch of the page does.
function Page() {
  const search = useSearch({ strict: false }) as { torrent?: string }
  const [shown, setShown] = useState<string | null>(search.torrent ?? null)
  const onClosed = useCallback(() => setShown(null), [])
  const details = useMobileDetailsHistory({ enabled: true, urlHash: search.torrent, shownHash: shown, onClosed })
  return (
    <>
      <output>{shown ?? "list"}</output>
      <button onClick={() => {
        setShown("a")
        details.open(prev => ({ ...prev, torrent: "a" }))
      }}>open</button>
      <button onClick={details.close}>close</button>
    </>
  )
}

function mount(entries: string[]) {
  const history = createMemoryHistory({ initialEntries: entries })
  const rootRoute = createRootRoute()
  const listRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/list",
    component: Page,
    validateSearch: (search: Record<string, unknown>) => ({ torrent: search.torrent as string | undefined }),
  })
  // A second list, as the list of another instance
  const list2Route = createRoute({
    getParentRoute: () => rootRoute,
    path: "/list2",
    component: Page,
    validateSearch: (search: Record<string, unknown>) => ({ torrent: search.torrent as string | undefined }),
  })
  const otherRoute = createRoute({ getParentRoute: () => rootRoute, path: "/other" })
  const router = createRouter({ routeTree: rootRoute.addChildren([listRoute, list2Route, otherRoute]), history })
  render(<RouterProvider router={router} />)
  return { history }
}

type Rig = ReturnType<typeof mount>

async function settle() {
  await act(async () => {
    await new Promise(resolve => setTimeout(resolve, 0))
  })
}

async function click(name: string) {
  await act(async () => screen.getByRole("button", { name }).click())
  await settle()
}

async function back({ history }: Rig) {
  await act(async () => history.back())
  await settle()
}

function shown() {
  return screen.getByRole("status").textContent
}

// One Back from the list leaves the page
function leavesInOneBack(rig: Rig) {
  expect(shown()).toBe("list")
  expect(rig.history.location.pathname).toBe("/list")
  expect(rig.history.location.search).toBe("")
  rig.history.back()
  expect(rig.history.location.pathname).toBe("/other")
}

afterEach(() => {
  cleanup()
  sessionStorage.clear()
})

describe("useMobileDetailsHistory", () => {
  it("closes the view on Back", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    expect(shown()).toBe("a")

    await back(rig)
    leavesInOneBack(rig)
  })

  it("goes back over its entry on close, so the next Back leaves the page", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await click("close")
    leavesInOneBack(rig)
  })

  it("closes on one Back when the same torrent opens a second time", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await click("close")
    await click("open")
    expect(shown()).toBe("a")

    await back(rig)
    leavesInOneBack(rig)
  })

  it("keeps its count over a reload with the view open", async () => {
    mount(["/other", "/list"])
    await settle()
    await click("open")
    cleanup()

    // A reload keeps the entries and their __TSR_index, and drops the rest of history.state
    const rig = mount(["/other", "/list", "/list?torrent=a"])
    await settle()
    expect(shown()).toBe("a")
    await click("close")
    leavesInOneBack(rig)
  })

  // regression: #3702
  it("closes on one Back when the same torrent opens again after a link from another page", async () => {
    // The link pushed no count, so the close replaces the URL in place
    const rig = mount(["/other", "/list?torrent=a"])
    await settle()
    await click("close")
    await click("open")
    expect(shown()).toBe("a")

    await back(rig)
    leavesInOneBack(rig)
  })

  // regression: #3703
  it("starts a new count when it opens from a list entry that Back left a count on", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await back(rig)
    // The list of another instance takes the history index of the closed view
    await act(async () => rig.history.push("/list2"))
    await settle()

    await click("open")
    await click("close")
    expect(rig.history.location.pathname).toBe("/list2")
    expect(shown()).toBe("list")
  })

  // regression: #3703
  it("closes in place when a link to a torrent takes the index of a view that Back left", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await back(rig)
    cleanup()

    // A link from another page lands on the index that the closed view had
    const linked = mount(["/other", "/list", "/list2?torrent=a"])
    await settle()
    expect(shown()).toBe("a")

    await click("close")
    expect(linked.history.location.pathname).toBe("/list2")
    expect(linked.history.location.search).toBe("")
    expect(shown()).toBe("list")
  })
})
