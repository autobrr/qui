/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider, useSearch } from "@tanstack/react-router"
import { act, cleanup, render, screen } from "@testing-library/react"
import { useCallback, useEffect, useState } from "react"
import { afterEach, describe, expect, it } from "vitest"

import { useMobileDetailsHistory } from "./useMobileDetailsHistory"

function Page() {
  const search = useSearch({ strict: false }) as { torrent?: string }
  const [shown, setShown] = useState<string | null>(search.torrent ?? null)
  useEffect(() => {
    if (search.torrent) setShown(search.torrent)
  }, [search.torrent])
  const onClosed = useCallback(() => setShown(null), [])
  const [mobile, setMobile] = useState(true)
  const details = useMobileDetailsHistory({ enabled: mobile, urlHash: search.torrent, shownHash: shown, onClosed })
  const open = (hash: string) => {
    setShown(hash)
    details.open(hash, prev => ({ ...prev, torrent: hash }))
  }
  return (
    <>
      <output>{shown ?? "list"}</output>
      <button onClick={() => open("a")}>open</button>
      <button onClick={() => open("b")}>open match</button>
      <button onClick={details.close}>close</button>
      <button onClick={() => setMobile(false)}>resize to desktop</button>
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
    staticData: { title: null },
    validateSearch: (search: Record<string, unknown>) => ({ torrent: search.torrent as string | undefined }),
  })
  const list2Route = createRoute({
    getParentRoute: () => rootRoute,
    path: "/list2",
    component: Page,
    staticData: { title: null },
    validateSearch: (search: Record<string, unknown>) => ({ torrent: search.torrent as string | undefined }),
  })
  const otherRoute = createRoute({ getParentRoute: () => rootRoute, path: "/other", staticData: { title: null } })
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

    const rig = mount(["/other", "/list", "/list?torrent=a"])
    await settle()
    expect(shown()).toBe("a")
    await click("close")
    leavesInOneBack(rig)
  })

  it("closes on one Back when the same torrent opens again after a link from another page", async () => {
    const rig = mount(["/other", "/list?torrent=a"])
    await settle()
    await click("close")
    await click("open")
    expect(shown()).toBe("a")

    await back(rig)
    leavesInOneBack(rig)
  })

  it("starts a new count when it opens from a list entry that Back left a count on", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await back(rig)
    await act(async () => rig.history.push("/list2"))
    await settle()

    await click("open")
    await click("close")
    expect(rig.history.location.pathname).toBe("/list2")
    expect(shown()).toBe("list")
  })

  it("closes in place when a link to a torrent takes the index of a view that Back left", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await back(rig)
    cleanup()

    const linked = mount(["/other", "/list", "/list2?torrent=b"])
    await settle()
    expect(shown()).toBe("b")

    await click("close")
    expect(linked.history.location.pathname).toBe("/list2")
    expect(linked.history.location.search).toBe("")
    expect(shown()).toBe("list")
  })

  it("goes back over its entry on close after Back and Forward", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await back(rig)
    await act(async () => rig.history.forward())
    await settle()
    expect(shown()).toBe("a")

    await click("close")
    leavesInOneBack(rig)
  })

  it("closes a view that a link opened when a close goes back to it from a match", async () => {
    const rig = mount(["/other", "/list?torrent=a"])
    await settle()
    await click("open match")
    expect(shown()).toBe("b")

    await click("close")
    leavesInOneBack(rig)
  })

  it("closes a view that a link opened when a close goes back to it from the same torrent", async () => {
    const rig = mount(["/other", "/list?torrent=a"])
    await settle()
    await click("open match")
    await click("open")
    expect(shown()).toBe("a")

    await click("close")
    leavesInOneBack(rig)
  })

  it("shows the next open after a close that went back to a view that a link opened", async () => {
    mount(["/other", "/list?torrent=a"])
    await settle()
    await click("open match")
    await click("open")
    await click("close")

    await click("open match")
    expect(shown()).toBe("b")
  })

  it("closes in place on the desktop layout after a resize", async () => {
    const rig = mount(["/other", "/list"])
    await settle()
    await click("open")
    await click("resize to desktop")

    await click("close")
    expect(shown()).toBe("list")
    expect(rig.history.location.search).toBe("")
    expect(rig.history.location.state.__TSR_index).toBe(2)
  })
})
