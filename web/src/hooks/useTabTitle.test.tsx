/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { act, cleanup, render } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, expect, it } from "vitest"

import { changeLanguage, initI18n } from "@/i18n"
import { usePageTitle } from "./usePageTitle"
import { useTabTitle } from "./useTabTitle"

function Root() {
  useTabTitle()
  return <Outlet />
}

const pageControls: { setDetail?: (detail: string | undefined) => void; setSpeeds?: (speeds: string | undefined) => void } = {}

function InstancePage() {
  const [detail, setDetail] = useState<string | undefined>()
  const [speeds, setSpeeds] = useState<string | undefined>()
  pageControls.setDetail = setDetail
  pageControls.setSpeeds = setSpeeds
  usePageTitle({ detail, speeds })
  return null
}

function mount(path: string) {
  const history = createMemoryHistory({ initialEntries: [path] })
  const rootRoute = createRootRoute({ component: Root, notFoundComponent: () => null, staticData: { title: null } })
  const statisticsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/statistics",
    staticData: { title: { titleKey: "nav.statistics", titleNs: "common" } },
  })
  const settingsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/settings",
    validateSearch: (search: Record<string, unknown>) => ({ tab: search.tab as string | undefined }),
    staticData: { title: { titleKey: "nav.settings", titleNs: "common" } },
  })
  const instanceRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/instances/1",
    component: InstancePage,
    staticData: { title: { titleKey: "nav.dashboard", titleNs: "common" } },
  })
  const untitledRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", staticData: { title: null } })
  const parentRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/parent",
    staticData: { title: { titleKey: "nav.settings", titleNs: "common" } },
  })
  const childRoute = createRoute({
    getParentRoute: () => parentRoute,
    path: "/child",
    staticData: { title: { titleKey: "nav.backups", titleNs: "common" } },
  })
  const untitledChildRoute = createRoute({ getParentRoute: () => parentRoute, path: "/untitled", staticData: { title: null } })
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      statisticsRoute,
      settingsRoute,
      instanceRoute,
      untitledRoute,
      parentRoute.addChildren([childRoute, untitledChildRoute]),
    ]),
    history,
  })
  render(<RouterProvider router={router} />)
  return router
}

async function settle() {
  await act(async () => {
    await new Promise(resolve => setTimeout(resolve, 0))
  })
}

function setTheme(themeId: string | null) {
  if (themeId) {
    document.documentElement.setAttribute("data-theme", themeId)
  } else {
    document.documentElement.removeAttribute("data-theme")
  }
  act(() => {
    window.dispatchEvent(new Event("themechange"))
  })
}

beforeEach(async () => {
  await initI18n()
  await changeLanguage("en")
})

afterEach(async () => {
  cleanup()
  document.documentElement.removeAttribute("data-theme")
  await changeLanguage("en")
})

it("names the tab after the route and qui", async () => {
  mount("/statistics")
  await settle()
  expect(document.title).toBe("Statistics | qui")
})

it.each(["notifications", "logs"])("keeps the page name on the %s tab of Settings", async (tab) => {
  mount(`/settings?tab=${tab}`)
  await settle()
  expect(document.title).toBe("Settings | qui")
})

it("shows only qui on a route without a title", async () => {
  mount("/")
  await settle()
  expect(document.title).toBe("qui")
})

it("shows only qui on the not-found page of a mistyped link", async () => {
  mount("/statistics/typo")
  await settle()
  expect(document.title).toBe("qui")
})

it("takes the title of the deepest route that has one", async () => {
  mount("/parent/child")
  await settle()
  expect(document.title).toBe("Backups | qui")
  cleanup()

  mount("/parent/untitled")
  await settle()
  expect(document.title).toBe("Settings | qui")
})

it("shows the route title until the page passes its detail", async () => {
  mount("/instances/1")
  await settle()
  expect(document.title).toBe("Dashboard | qui")

  act(() => pageControls.setDetail?.("seedbox"))
  expect(document.title).toBe("seedbox | qui")
})

it("puts the speeds first", async () => {
  mount("/instances/1")
  await settle()
  act(() => {
    pageControls.setDetail?.("seedbox")
    pageControls.setSpeeds?.("D: 1 MiB/s U: 2 MiB/s")
  })
  expect(document.title).toBe("D: 1 MiB/s U: 2 MiB/s | seedbox | qui")
})

it("drops the detail and the speeds of a page that is left", async () => {
  const router = mount("/instances/1")
  await settle()
  act(() => {
    pageControls.setDetail?.("seedbox")
    pageControls.setSpeeds?.("D: 1 MiB/s U: 2 MiB/s")
  })

  await act(() => router.navigate({ to: "/statistics" }))
  await settle()
  expect(document.title).toBe("Statistics | qui")
})

it("follows a language switch", async () => {
  mount("/statistics")
  await settle()
  await act(() => changeLanguage("de"))
  expect(document.title).toBe("Statistiken | qui")
})

it("shows the spreadsheet title while a spreadsheet theme is on", async () => {
  mount("/instances/1")
  await settle()
  act(() => pageControls.setSpeeds?.("D: 1 MiB/s U: 2 MiB/s"))

  setTheme("spreadsheet")
  expect(document.title).toBe("Book1.xlsx")
  setTheme("spreadsheet-classic")
  expect(document.title).toBe("Book1.xls")
  setTheme(null)
  expect(document.title).toBe("D: 1 MiB/s U: 2 MiB/s | Dashboard | qui")
})
