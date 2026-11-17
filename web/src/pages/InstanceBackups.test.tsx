/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { delay, HttpResponse, http } from "msw"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { server } from "@/test/msw/server"

const stubs = vi.hoisted(() => ({
  translation: { t: (key: string) => key, i18n: { language: "en", resolvedLanguage: "en" } },
  selectInstance: (() => {}) as (id: number) => void,
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => stubs.translation,
}))

vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/contexts/SyncStreamContext")>(),
  useActivityStream: () => {},
}))

vi.mock("@/hooks/usePersistedInstanceSelection", () => ({
  usePersistedInstanceSelection: () => {
    const [id, setId] = useState<number | undefined>(1)
    stubs.selectInstance = setId
    return [id, setId] as const
  },
}))

import { InstanceBackups } from "./InstanceBackups"

afterEach(cleanup)

const settings = (instanceId: number) => ({
  instanceId,
  enabled: true,
  hourlyEnabled: false,
  dailyEnabled: true,
  weeklyEnabled: false,
  monthlyEnabled: false,
  keepHourly: 0,
  keepDaily: 7,
  keepWeekly: 4,
  keepMonthly: 12,
  includeCategories: true,
  includeTags: true,
  includeSavePaths: true,
})

describe("InstanceBackups", () => {
  it("keeps Save disabled while the next instance's settings load", async () => {
    server.use(
      http.get("*/api/instances", () => HttpResponse.json([
        { id: 1, name: "A", host: "http://a.test", connected: true },
        { id: 2, name: "B", host: "http://b.test", connected: true },
      ])),
      http.get("*/api/instances/:id/capabilities", () => HttpResponse.json({ supportsTorrentExport: true })),
      http.get("*/api/instances/:id/backups/runs", () => HttpResponse.json({ runs: [], hasMore: false })),
      http.get("*/api/instances/:id/backups/settings", async ({ params }) => {
        if (params.id === "2") await delay("infinite")
        return HttpResponse.json(settings(Number(params.id)))
      })
    )
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
    render(
      <RouterContextProvider router={router}>
        <QueryClientProvider client={queryClient}>
          <InstanceBackups />
        </QueryClientProvider>
      </RouterContextProvider>
    )
    const save = () => screen.getByRole("button", { name: "backups.settings.actions.saveChanges" })
    await waitFor(() => expect(save().hasAttribute("disabled")).toBe(false))

    act(() => stubs.selectInstance(2))

    expect(save().hasAttribute("disabled")).toBe(true)
    expect(screen.getByRole("button", { name: "backups.settings.actions.saveAll" }).hasAttribute("disabled")).toBe(true)
  })
})
