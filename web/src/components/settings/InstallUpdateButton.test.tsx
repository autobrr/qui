/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { type AnyRouter, createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { UpdateBanner } from "@/components/ui/UpdateBanner"
import { api } from "@/lib/api"
import { server } from "@/test/msw/server"

// Appends the interpolation values, so a test can find the backupError text.
const i18n = vi.hoisted(() => ({
  t: (key: string, options?: Record<string, unknown>) => [key, ...Object.values(options ?? {})].join(" "),
}))
vi.mock("react-i18next", () => ({ useTranslation: () => i18n }))

const OLD_STARTED_AT = "2026-09-28T20:00:00Z"
const NEW_STARTED_AT = "2026-09-28T21:00:00Z"
const RELEASE = { tag_name: "v1.31.0", html_url: "https://github.com/autobrr/qui/releases/tag/v1.31.0", published_at: "2026-09-28T00:00:00Z" }

const location = { assign: vi.fn(), reload: vi.fn() }
const serviceWorker = { getRegistrations: vi.fn() }
const cacheStorage = { keys: vi.fn(), delete: vi.fn() }
let quiRegistration = { scope: "", unregister: vi.fn() }
let otherRegistration = { scope: "", unregister: vi.fn() }

let qui: "old" | "down" | "new" = "old"
let newVersion = "1.31.0"
let selfUpdateAvailable = true
let updateResult = { version: "1.31.0", rollbackCommand: "mv \"/opt/qui/qui-v1.30.0.bak\" \"/opt/qui/qui\"", backupError: "" }
let updateBodies: unknown[] = []
let queryClient: QueryClient
let router: AnyRouter

function renderBanner() {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory({ initialEntries: ["/settings"] }) })
  return render(
    <RouterContextProvider router={router}>
      <QueryClientProvider client={queryClient}>
        <UpdateBanner />
      </QueryClientProvider>
    </RouterContextProvider>
  )
}

async function navigateTo(path: string) {
  await act(async () => {
    router.history.push(path)
  })
  return router.history.location.pathname
}

async function confirmInstall() {
  fireEvent.click(await screen.findByText("application.update.button"))
  fireEvent.click(screen.getByText("application.update.confirm"))
}

beforeEach(() => {
  qui = "old"
  newVersion = "1.31.0"
  selfUpdateAvailable = true
  updateResult = { version: "1.31.0", rollbackCommand: "mv \"/opt/qui/qui-v1.30.0.bak\" \"/opt/qui/qui\"", backupError: "" }
  updateBodies = []
  sessionStorage.clear()
  localStorage.clear()
  vi.stubGlobal("location", { ...window.location, ...location, origin: window.location.origin, pathname: "/" })

  quiRegistration = { scope: new URL("/", window.location.origin).href, unregister: vi.fn(async () => true) }
  otherRegistration = { scope: new URL("/photos/", window.location.origin).href, unregister: vi.fn(async () => true) }
  serviceWorker.getRegistrations.mockResolvedValue([quiRegistration, otherRegistration])
  Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: serviceWorker })
  cacheStorage.keys.mockResolvedValue([`workbox-precache-v2-${quiRegistration.scope}`, `workbox-precache-v2-${otherRegistration.scope}`])
  cacheStorage.delete.mockResolvedValue(true)
  vi.stubGlobal("caches", cacheStorage)

  server.use(
    http.get("*/api/version/latest", () => qui === "down" ? HttpResponse.error() : HttpResponse.json(qui === "new" ? null : RELEASE)),
    http.get("*/api/version", () => HttpResponse.json({ version: "1.30.0", updateAvailable: true, selfUpdate: selfUpdateAvailable, restart: true })),
    http.get("*/api/application/info", () => {
      if (qui === "down") {
        return HttpResponse.error()
      }
      return HttpResponse.json(qui === "old" ? { startedAt: OLD_STARTED_AT, version: "1.30.0" } : { startedAt: NEW_STARTED_AT, version: newVersion })
    }),
    http.post("*/api/system/update", async ({ request }) => {
      updateBodies.push(await request.json())
      return HttpResponse.json(updateResult)
    })
  )
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  Reflect.deleteProperty(navigator, "serviceWorker")
})

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

describe("Install update", () => {
  it("shows no button when Self-update is not available", async () => {
    selfUpdateAvailable = false
    renderBanner()

    await screen.findByText("updateBanner.viewRelease")
    await act(async () => {})
    expect(screen.queryByText("application.update.button")).toBeNull()
  })

  it("shows both versions and the release notes, and installs the tag it showed", async () => {
    renderBanner()
    fireEvent.click(await screen.findByText("application.update.button"))

    expect(await screen.findByText("1.30.0")).toBeTruthy()
    expect(screen.getByText("v1.31.0")).toBeTruthy()
    const notes = screen.getByText("application.update.releaseNotes").closest("a")
    expect(notes?.getAttribute("href")).toBe(RELEASE.html_url)

    fireEvent.click(screen.getByText("application.update.confirm"))
    await screen.findByText("application.restart.overlay.restartingTitle")
    expect(updateBodies).toEqual([{ version: "v1.31.0" }])
  })

  it.each([
    [502, "GitHub API rate limit exceeded for 203.0.113.7"],
    [409, "An update or restart is already running"],
  ])("shows a %i error message as it is and no overlay", async (status, message) => {
    server.use(http.post("*/api/system/update", () => HttpResponse.json({ error: message }, { status })))
    renderBanner()

    await confirmInstall()

    expect(await screen.findByText(message)).toBeTruthy()
    expect(screen.queryByText("application.restart.overlay.restartingTitle")).toBeNull()
  })

  it.each(["1.31.0", "v1.31.0"])("clears the service worker, then reloads once when the new process reports %s", async (reported) => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    newVersion = reported
    localStorage.setItem("qui-theme", "dark")
    let unregisteredAtReload = -1
    location.reload.mockImplementation(() => {
      unregisteredAtReload = quiRegistration.unregister.mock.calls.length
    })
    renderBanner()
    await confirmInstall()
    await screen.findByText("application.restart.overlay.restartingTitle")

    // The old process still answers while its graceful shutdown waits.
    await advance(3_000)
    expect(location.reload).not.toHaveBeenCalled()
    expect(screen.queryByText("application.update.overlay.notAppliedTitle")).toBeNull()

    qui = "down"
    // Without the pause, the SSO recovery sends the tab to "/" and the overlay is gone.
    await expect(api.getApplicationInfo()).rejects.toThrow(TypeError)
    expect(location.assign).not.toHaveBeenCalled()
    // The update check fails while qui is down, and the banner unmounts.
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ["latest-version"] })
    })
    await vi.waitFor(() => expect(screen.queryByText("updateBanner.viewRelease")).toBeNull())
    expect(screen.getByText("application.restart.overlay.restartingTitle")).toBeTruthy()

    qui = "new"
    await advance(3_000)
    expect(location.reload).toHaveBeenCalledTimes(1)
    expect(unregisteredAtReload).toBe(1)
    expect(otherRegistration.unregister).not.toHaveBeenCalled()
    expect(cacheStorage.delete.mock.calls).toEqual([[`workbox-precache-v2-${quiRegistration.scope}`]])
    expect(localStorage.getItem("qui-theme")).toBe("dark")
    expect(location.assign).not.toHaveBeenCalled()
  })

  it("shows \"Update did not apply\" when qui comes back on another version", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    newVersion = "1.30.0"
    renderBanner()
    await confirmInstall()
    await screen.findByText("application.restart.overlay.restartingTitle")

    qui = "new"
    await advance(3_000)
    expect(screen.getByText("application.update.overlay.notAppliedTitle")).toBeTruthy()
    expect(location.reload).not.toHaveBeenCalled()
    expect(serviceWorker.getRegistrations).not.toHaveBeenCalled()

    // The 60-second text must not replace the result.
    await advance(60_000)
    expect(screen.getByText("application.update.overlay.notAppliedTitle")).toBeTruthy()

    fireEvent.click(screen.getByText("application.update.overlay.reload"))
    expect(location.reload).toHaveBeenCalledTimes(1)
  })

  it("shows the rollback command after 60 seconds and keeps polling", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderBanner()
    await confirmInstall()
    await screen.findByText("application.restart.overlay.restartingTitle")
    qui = "down"

    await advance(58_000)
    expect(screen.queryByText(updateResult.rollbackCommand)).toBeNull()

    await advance(3_000)
    expect(screen.getByText("application.restart.overlay.slowTitle")).toBeTruthy()
    expect(screen.getByText(updateResult.rollbackCommand)).toBeTruthy()
    expect(screen.getByTitle(/^application\.copyTitle/)).toBeTruthy()
    const docs = screen.getByText("application.update.overlay.rollbackDocs").closest("a")
    expect(docs?.getAttribute("href")).toContain("#roll-back-an-update")
    expect(screen.queryByText(/application\.update\.overlay\.noBackup/)).toBeNull()

    qui = "new"
    await advance(3_000)
    expect(location.reload).toHaveBeenCalledTimes(1)
  })

  it("warns when qui updated but kept no backup", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    updateResult = { version: "1.31.0", rollbackCommand: "", backupError: "backup not found at /opt/qui/qui-v1.30.0.bak" }
    renderBanner()
    await confirmInstall()

    expect(await screen.findByText("application.update.overlay.noBackup backup not found at /opt/qui/qui-v1.30.0.bak")).toBeTruthy()
    qui = "down"
    await advance(61_000)
    expect(screen.getByText("application.restart.overlay.slowTitle")).toBeTruthy()
    expect(screen.queryByText("application.update.overlay.rollbackIntro")).toBeNull()
  })

  it("blocks navigation while the install runs and while the overlay shows", async () => {
    let finishUpdate = () => {}
    const updateRequested = new Promise<void>((requested) => {
      server.use(http.post("*/api/system/update", async () => {
        requested()
        await new Promise<void>((resolve) => {
          finishUpdate = resolve
        })
        return HttpResponse.json(updateResult)
      }))
    })
    renderBanner()
    expect(await navigateTo("/dashboard")).toBe("/dashboard")

    await confirmInstall()
    await updateRequested
    await vi.waitFor(() => expect(screen.getByText("common:actions.cancel").closest("button")?.disabled).toBe(true))
    expect(await navigateTo("/settings")).toBe("/dashboard")

    finishUpdate()
    await screen.findByText("application.restart.overlay.restartingTitle")
    expect(await navigateTo("/settings")).toBe("/dashboard")
  })
})
