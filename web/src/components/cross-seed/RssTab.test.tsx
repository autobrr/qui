/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  settings: {
    enabled: true,
    runIntervalMinutes: 120,
    targetInstanceIds: [1],
    targetIndexerIds: [],
    rssSourceCategories: ["movies"],
    rssSourceTags: [],
    rssSourceExcludeCategories: [],
    rssSourceExcludeTags: [],
    rssAutomationTags: ["cross-seed", "rss"],
    skipAutoResumeRss: false,
    webhookTags: ["autobrr"],
    gazelleEnabled: true,
  },
  patchSettings: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    getCrossSeedStatus: () => Promise.resolve({ settings: mocks.settings, running: false, lastRun: null }),
    getInstances: () => Promise.resolve([{ id: 1, name: "main", isActive: true }]),
    listTorznabIndexers: () => Promise.resolve([{ id: 7, name: "idx", enabled: true, indexer_id: "idx", base_url: "http://idx.test" }]),
    listCrossSeedRuns: () => Promise.resolve([]),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { RssTab } from "./RssTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RssTab />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

async function mountedInterval(container: HTMLElement) {
  return waitFor(() => {
    const input = container.querySelector<HTMLInputElement>("#automation-interval")
    if (!input) throw new Error("card not mounted yet")
    return input
  })
}

describe("RssTab save", () => {
  it("sends every RSS field and nothing from another card", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const { container } = renderTab()

    const interval = await mountedInterval(container)
    fireEvent.change(interval, { target: { value: "240" } })
    fireEvent.click(screen.getByRole("switch", { name: "sourceCard.autoResume" }))
    fireEvent.click(screen.getByRole("button", { name: "automation.saveSettings" }))

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      enabled: true,
      runIntervalMinutes: 240,
      targetInstanceIds: [1],
      targetIndexerIds: [],
      rssSourceCategories: ["movies"],
      rssSourceTags: [],
      rssSourceExcludeCategories: [],
      rssSourceExcludeTags: [],
      rssAutomationTags: ["cross-seed", "rss"],
      skipAutoResumeRss: true,
    })
  })

  it("rejects an interval under the minimum before it sends anything", async () => {
    const { container } = renderTab()
    const interval = await mountedInterval(container)
    fireEvent.change(interval, { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: "automation.saveSettings" }))

    expect(screen.getByText("validation.minInterval")).toBeTruthy()
    expect(mocks.patchSettings).not.toHaveBeenCalled()
  })
})
