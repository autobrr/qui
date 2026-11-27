/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import type { Instance } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: { instances: [{ id: 1, name: "music", isActive: true }] },
  updateSettings: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/hooks/useInstances", () => ({
  useInstances: () => mocks.instances as unknown as { instances: Instance[] },
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstanceCompletionSettings: () => Promise.resolve({
      instanceId: 1,
      enabled: true,
      categories: [],
      tags: [],
      excludeCategories: [],
      excludeTags: [],
      indexerIds: [],
      gazelleSites: ["redacted.sh"],
      bypassTorznabCache: false,
      delaySeconds: 0,
    }),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    listTorznabIndexers: () => Promise.resolve([
      { id: 7, name: "Music Indexer", enabled: true, priority: 0 },
      { id: 8, name: "Redacted", enabled: true, priority: 0 },
    ]),
    getCrossSeedGazelleSites: () => Promise.resolve([
      { site: "redacted.sh", name: "Redacted (API)", shortName: "RED", indexerId: -1001, active: true, replacedIndexerIds: [8] },
      { site: "orpheus.network", name: "Orpheus (API)", shortName: "OPS", indexerId: -1002, active: true, replacedIndexerIds: [] },
    ]),
    updateInstanceCompletionSettings: mocks.updateSettings,
  },
}))

import { CompletionOverview } from "./CompletionOverview"

describe("CompletionOverview", () => {
  it("shows a saved RED-only pick in the picker and saves it back whole", async () => {
    mocks.updateSettings.mockImplementation((_id: number, settings: object) => Promise.resolve({ instanceId: 1, ...settings }))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <CompletionOverview />
        </TooltipProvider>
      </QueryClientProvider>
    )

    fireEvent.click(await screen.findByText("music"))
    expect(await screen.findByText("Redacted (API)")).toBeTruthy()
    expect(screen.getByText("preferences.completionOverview.selectedIndexers")).toBeTruthy()

    const toggle = screen.getAllByRole("switch")[0]
    await waitFor(() => expect(toggle.hasAttribute("disabled")).toBe(false))
    fireEvent.click(toggle)

    await waitFor(() => expect(mocks.updateSettings).toHaveBeenCalledTimes(1))
    expect(mocks.updateSettings.mock.calls[0][1]).toMatchObject({ enabled: false, indexerIds: [], gazelleSites: ["redacted.sh"] })
  })
})
