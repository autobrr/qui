/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { server } from "@/test/msw/server"
import { Setup } from "./Setup"

const mocks = vi.hoisted(() => ({
  translation: { t: (key: string) => key },
  navigate: vi.fn(),
  setup: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-router")>(),
  useNavigate: () => mocks.navigate,
}))

vi.mock("@/hooks/useAuth", () => ({
  useAuth: () => ({
    setup: mocks.setup,
    isSettingUp: false,
    setupError: null,
  }),
}))

class StubIntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

let queryClient: QueryClient

function useSetupResponses() {
  server.use(
    http.get("*/api/auth/oidc/config", () => HttpResponse.json({ enabled: false }))
  )
}

function renderSetup() {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <Setup />
    </QueryClientProvider>
  )
}

const createAccount = () => screen.queryByRole("button", { name: "setup.createAccount" })

afterEach(() => {
  cleanup()
  queryClient?.clear()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

beforeEach(() => {
  vi.stubGlobal("IntersectionObserver", StubIntersectionObserver)
  useSetupResponses()
})

describe("Setup", () => {
  it("sends a signed-out visitor of a configured install to the login page", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ setupRequired: false })))

    renderSetup()

    await vi.waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/login" }))
    expect(createAccount()).toBeNull()
  })

  it("shows the account form on a fresh install", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ setupRequired: true })))

    renderSetup()

    expect(await screen.findByRole("button", { name: "setup.createAccount" })).toBeTruthy()
    expect(mocks.navigate).not.toHaveBeenCalled()
  })

  // regression: #4313
  it("shows the check error instead of the account form", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ error: "database unavailable" }, { status: 500 })))

    renderSetup()

    expect(await screen.findByRole("alert")).toBeTruthy()
    expect(screen.getByText("setup.checkFailed")).toBeTruthy()
    expect(createAccount()).toBeNull()
    expect(mocks.navigate).not.toHaveBeenCalled()
  })

  // regression: #4313
  it("retries the check after a failure", async () => {
    let attempts = 0
    server.use(http.get("*/api/auth/check-setup", () => {
      attempts += 1
      return attempts === 1
        ? HttpResponse.json({ error: "database unavailable" }, { status: 500 })
        : HttpResponse.json({ setupRequired: true })
    }))

    renderSetup()

    const retry = await screen.findByRole("button", { name: "setup.retry" })
    fireEvent.click(retry)

    expect(await screen.findByRole("button", { name: "setup.createAccount" })).toBeTruthy()
    expect(attempts).toBe(2)
  })
})
