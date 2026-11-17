/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { useState } from "react"
import { afterEach, expect, it, vi } from "vitest"

import "@/i18n"
import type { SettingsSearch } from "@/routes/_authenticated/settings"
import { server } from "@/test/msw/server"
import { Settings } from "./Settings"

let client: QueryClient

afterEach(() => {
  cleanup()
  client.clear()
})

it("opens update guidance from a menu link when application-info fails", async () => {
  const consumeRequest = vi.fn()
  const updateRequest = vi.fn()
  server.use(
    http.get("*/api/application/info", () => HttpResponse.json({ error: "Application info unavailable" }, { status: 500 })),
    http.get("*/api/auth/me", () => HttpResponse.json({ username: "demo", auth_method: "builtin" })),
    http.get("*/api/version/latest", () => HttpResponse.json({ tag_name: "v1.31.1", html_url: "https://example.invalid/releases/v1.31.1" })),
    http.get("*/api/version", () => HttpResponse.json({ version: "1.30.0", selfUpdate: false, selfUpdateUnavailableReason: "disabled", restart: true })),
    http.post("*/api/system/update", () => {
      updateRequest()
      return HttpResponse.json({})
    })
  )
  function Page() {
    const [search, setSearch] = useState<SettingsSearch>({ tab: "application", modal: "install-update" })
    return <Settings search={search} onSearchChange={(next) => { consumeRequest(next); setSearch(next) }} />
  }
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory({ initialEntries: ["/settings"] }) })
  render(
    <RouterContextProvider router={router}>
      <QueryClientProvider client={client}><Page /></QueryClientProvider>
    </RouterContextProvider>
  )

  expect(await screen.findByText("Application info unavailable")).toBeTruthy()
  expect(await screen.findByText("Self-update is disabled in the qui configuration.")).toBeTruthy()
  expect(screen.getByRole("alertdialog")).toBeTruthy()
  expect(consumeRequest).toHaveBeenCalledWith({ tab: "application", modal: undefined })
  expect(screen.getByText("v1.30.0")).toBeTruthy()
  expect(screen.getByText("v1.31.1")).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Close" }))
  expect(screen.queryByRole("alertdialog")).toBeNull()
  expect(updateRequest).not.toHaveBeenCalled()
})
