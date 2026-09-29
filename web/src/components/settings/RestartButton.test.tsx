/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { RestartButton } from "@/components/settings/RestartButton"
import { api } from "@/lib/api"
import { server } from "@/test/msw/server"

const i18n = vi.hoisted(() => ({ t: (key: string) => key }))
vi.mock("react-i18next", () => ({ useTranslation: () => i18n }))

const location = { assign: vi.fn(), reload: vi.fn() }
const serviceWorker = { getRegistrations: vi.fn(async () => []) }
const cacheStorage = { keys: vi.fn(async () => []), delete: vi.fn(async () => true) }

let versionCalls = 0
let infoCalls = 0
let qui: "old" | "down" | "moved" | "new" = "old"
let restartAvailable = true

function renderButton() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
  return render(
    <RouterContextProvider router={router}>
      <QueryClientProvider client={queryClient}>
        <RestartButton />
      </QueryClientProvider>
    </RouterContextProvider>
  )
}

async function confirmRestart() {
  fireEvent.click(await screen.findByText("application.restart.button"))
  fireEvent.click(screen.getByText("application.restart.confirm"))
}

beforeEach(() => {
  versionCalls = 0
  infoCalls = 0
  qui = "old"
  restartAvailable = true
  sessionStorage.clear()
  vi.stubGlobal("location", { ...window.location, ...location, origin: window.location.origin, pathname: "/" })
  Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: serviceWorker })
  vi.stubGlobal("caches", cacheStorage)
  server.use(
    http.get("*/api/version", () => {
      versionCalls++
      return HttpResponse.json({ version: "1.30.0", updateAvailable: false, selfUpdate: false, restart: restartAvailable })
    }),
    http.get("*/api/application/info", () => {
      infoCalls++
      if (qui === "down") {
        return HttpResponse.error()
      }
      if (qui === "moved") {
        return new HttpResponse("404 page not found", { status: 404 })
      }
      return HttpResponse.json({ startedAt: qui === "old" ? "2026-09-28T20:00:00Z" : "2026-09-28T21:00:00Z" })
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

describe("RestartButton", () => {
  it("shows no button when qui cannot restart", async () => {
    restartAvailable = false
    renderButton()

    await vi.waitFor(() => expect(versionCalls).toBe(1))
    await act(async () => {})
    expect(screen.queryByText("application.restart.button")).toBeNull()
  })

  it("shows a 409 message in the dialog and no overlay", async () => {
    server.use(http.post("*/api/system/restart", () => HttpResponse.json({ error: "An update or restart is already running" }, { status: 409 })))
    renderButton()

    await confirmRestart()

    expect(await screen.findByText("An update or restart is already running")).toBeTruthy()
    expect(screen.queryByText("application.restart.overlay.restartingTitle")).toBeNull()
  })

  it("keeps the dialog open on Escape while the restart request runs", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    let accept = () => {}
    const accepted = new Promise<void>((resolve) => {
      accept = resolve
    })
    server.use(http.post("*/api/system/restart", async () => {
      await accepted
      return new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } })
    }))
    renderButton()
    await confirmRestart()
    await vi.waitFor(() => expect(infoCalls).toBe(1))

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(screen.getByText("application.restart.confirmTitle")).toBeTruthy()

    // qui can close its listener before the 202 arrives.
    qui = "down"
    await expect(api.getApplicationInfo()).rejects.toThrow(TypeError)
    expect(location.assign).not.toHaveBeenCalled()
    qui = "old"

    accept()
    await screen.findByText("application.restart.overlay.restartingTitle")
    // The old process still answers, so the overlay must not reload yet.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(location.reload).not.toHaveBeenCalled()
  })

  it("ignores the Restart button while the overlay shows", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    server.use(http.post("*/api/system/restart", () => new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } })))
    renderButton()
    await confirmRestart()
    await screen.findByText("application.restart.overlay.restartingTitle")

    // fireEvent reaches the button under the overlay; a real click does not.
    fireEvent.click(screen.getByText("application.restart.button"))
    fireEvent.keyDown(document.body, { key: "Escape" })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(screen.queryByText("application.restart.confirmTitle")).toBeNull()
    expect(location.reload).not.toHaveBeenCalled()
  })

  it("keeps focus in the overlay and hides the page under it", async () => {
    server.use(http.post("*/api/system/restart", () => new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } })))
    renderButton()
    await confirmRestart()
    await screen.findByText("application.restart.overlay.restartingTitle")

    // The closed confirm dialog returns focus to the Restart button on a timer.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
    })
    // A Settings tab switch under the overlay would unmount the poll.
    expect(screen.getByRole("dialog").contains(document.activeElement)).toBe(true)
    expect(screen.queryByRole("button", { name: "application.restart.button" })).toBeNull()
  })

  it("keeps the overlay until a new qui process answers, changes the text after 60 seconds, and reloads once", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    server.use(http.post("*/api/system/restart", () => new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } })))
    renderButton()
    await confirmRestart()
    await screen.findByText("application.restart.overlay.restartingTitle")

    // The old process still answers while its graceful shutdown waits.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(location.reload).not.toHaveBeenCalled()

    qui = "down"
    // Any API request fails with "Failed to fetch" while qui is down. Without
    // the pause, the SSO recovery sends the tab to "/" and the overlay is gone.
    await expect(api.getApplicationInfo()).rejects.toThrow(TypeError)
    expect(location.assign).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(55_000)
    })
    expect(screen.getByText("application.restart.overlay.restartingTitle")).toBeTruthy()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(screen.getByText("application.restart.overlay.slowTitle")).toBeTruthy()
    expect(screen.queryByText("application.restart.overlay.restartingTitle")).toBeNull()

    const callsAtSlow = infoCalls
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(infoCalls).toBeGreaterThan(callsAtSlow)
    expect(location.reload).not.toHaveBeenCalled()

    // After a base URL change, the old path answers 404 and the slow text stays.
    qui = "moved"
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(location.reload).not.toHaveBeenCalled()

    qui = "new"
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(location.reload).toHaveBeenCalledTimes(1)
    expect(location.assign).not.toHaveBeenCalled()
    // Only a Self-update changes the frontend.
    expect(serviceWorker.getRegistrations).not.toHaveBeenCalled()
    expect(cacheStorage.keys).not.toHaveBeenCalled()
  })
})
