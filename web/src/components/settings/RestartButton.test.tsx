/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { RestartButton } from "@/components/settings/RestartButton"
import { api } from "@/lib/api"
import { server } from "@/test/msw/server"

const i18n = vi.hoisted(() => ({ t: (key: string) => key }))
vi.mock("react-i18next", () => ({ useTranslation: () => i18n }))

const location = { assign: vi.fn(), reload: vi.fn() }

let versionCalls = 0
let infoCalls = 0
let qui: "old" | "down" | "new" = "old"
let restartAvailable = true

function renderButton() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <RestartButton />
    </QueryClientProvider>
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
      return HttpResponse.json({ startedAt: qui === "old" ? "2026-09-28T20:00:00Z" : "2026-09-28T21:00:00Z" })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
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

    qui = "new"
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(location.reload).toHaveBeenCalledTimes(1)
    expect(location.assign).not.toHaveBeenCalled()
  })
})
