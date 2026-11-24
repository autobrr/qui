/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { TorznabIndexer } from "@/types"

const INDEXER: TorznabIndexer = {
  id: 1,
  name: "Example indexer",
  base_url: "http://localhost:9117",
  indexer_id: "example",
  backend: "jackett",
  enabled: true,
  priority: 0,
  timeout_seconds: 30,
  capabilities: [],
  categories: [],
  last_test_status: "",
  created_at: "",
  updated_at: "",
}

const mocks = vi.hoisted(() => ({
  stored: null as TorznabIndexer | null,
  syncTorznabCaps: vi.fn(),
  toastSuccess: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: mocks.toastSuccess, warning: vi.fn() } }))
vi.mock("@/components/indexers/IndexerActivityPanel", () => ({ IndexerActivityPanel: () => null }))
vi.mock("@/components/indexers/SearchHistoryPanel", () => ({ SearchHistoryPanel: () => null }))
vi.mock("@/lib/api", () => ({
  api: {
    listTorznabIndexers: () => Promise.resolve([mocks.stored]),
    updateTorznabIndexer: (_id: number, data: Partial<TorznabIndexer>) => {
      mocks.stored = { ...mocks.stored!, ...data }
      return Promise.resolve(mocks.stored)
    },
    syncTorznabCaps: mocks.syncTorznabCaps,
  },
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import { IndexersPage } from "./IndexersPage"

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <IndexersPage />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe("IndexersPage", () => {
  // regression: #4233
  it("keeps a newer switch state when a capability sync answers late", async () => {
    mocks.stored = INDEXER
    let finishSync: (indexer: TorznabIndexer) => void = () => {}
    mocks.syncTorznabCaps.mockImplementation(() => new Promise((resolve) => { finishSync = resolve }))
    renderPage()

    const toggle = await screen.findByRole("switch", { name: "kit.enabled" })
    fireEvent.click(screen.getByRole("button", { name: "indexers.table.tooltipSyncCaps" }))
    fireEvent.click(toggle)
    await waitFor(() => expect([toggle.getAttribute("aria-checked"), toggle.hasAttribute("disabled")]).toEqual(["false", false]))

    // The server read the row for this answer before the switch saved.
    finishSync({ ...INDEXER, enabled: true, capabilities: ["search"] })
    await waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalledWith("indexers.toast.capsSynced"))

    expect(screen.getByRole("switch", { name: "kit.enabled" }).getAttribute("aria-checked")).toBe("false")
    expect(screen.queryByText("indexers.list.capabilities")).not.toBeNull()
  })
})
