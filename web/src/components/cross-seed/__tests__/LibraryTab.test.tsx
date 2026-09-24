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
    seededSearchTags: ["cross-seed"],
    skipAutoResumeSeededSearch: false,
    gazelleEnabled: false,
    orpheusApiKey: "",
    redactedApiKey: "",
    seasonPackAutomationEnabled: false,
  },
  searchSettings: { instanceId: 1, categories: [], tags: [], indexerIds: [] as number[], intervalSeconds: 60, cooldownMinutes: 720 },
  instances: [{ id: 1, name: "main", isActive: true }],
  indexers: [] as { id: number; indexer_id: string; name: string; base_url: string; enabled: boolean }[],
  patchSearch: vi.fn(),
  patchSettings: vi.fn(),
  startRun: vi.fn(),
  getSearchSettings: vi.fn(() => Promise.resolve(mocks.searchSettings)),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSearchSettings: mocks.getSearchSettings,
    getInstances: () => Promise.resolve(mocks.instances),
    listTorznabIndexers: () => Promise.resolve(mocks.indexers),
    getCrossSeedSearchStatus: () => Promise.resolve({ running: false }),
    listCrossSeedSearchRuns: () => Promise.resolve([]),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    patchCrossSeedSearchSettings: mocks.patchSearch,
    patchCrossSeedSettings: mocks.patchSettings,
    startCrossSeedSearchRun: mocks.startRun,
  },
}))

import type { CrossSeedAutomationSettings } from "@/types"
import { LibraryTab } from "../LibraryTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  Object.assign(mocks.settings, { gazelleEnabled: false, orpheusApiKey: "", redactedApiKey: "" })
  mocks.searchSettings.indexerIds = []
  mocks.indexers = []
})

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <LibraryTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe("LibraryTab save", () => {
  it("sends the search settings and only the library fields of the cross-seed settings", async () => {
    mocks.patchSearch.mockResolvedValue({ ...mocks.searchSettings, cooldownMinutes: 900 })
    mocks.patchSettings.mockResolvedValue({ ...mocks.settings, skipAutoResumeSeededSearch: true })
    const { container } = renderTab()

    const cooldown = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#search-cooldown")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(cooldown, { target: { value: "900" } })
    fireEvent.click(screen.getByRole("switch", { name: "sourceCard.autoResume" }))
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.patchSearch).toHaveBeenCalledTimes(1))
    expect(mocks.patchSearch).toHaveBeenCalledWith({
      instanceId: 1,
      categories: [],
      tags: [],
      indexerIds: [],
      intervalSeconds: 60,
      cooldownMinutes: 900,
    })
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      seededSearchTags: ["cross-seed"],
      skipAutoResumeSeededSearch: true,
    })
  })

  it("refetches the search settings when one of the two saves fails", async () => {
    mocks.patchSearch.mockRejectedValue(new Error("boom"))
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    renderTab()
    await waitFor(() => expect(mocks.getSearchSettings).toHaveBeenCalledTimes(1))
    fireEvent.click(await screen.findByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.getSearchSettings).toHaveBeenCalledTimes(2))
  })

  it("associates the tags label with the picker trigger", async () => {
    renderTab()
    const trigger = await screen.findByLabelText("sourceCard.crossSeedTags")
    expect(trigger.getAttribute("role")).toBe("combobox")
  })

  it("rejects a cooldown under the minimum before it sends anything", async () => {
    const { container } = renderTab()
    const cooldown = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#search-cooldown")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(cooldown, { target: { value: "10" } })
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    expect(screen.getByText("validation.minCooldown")).toBeTruthy()
    expect(mocks.patchSearch).not.toHaveBeenCalled()
    expect(mocks.patchSettings).not.toHaveBeenCalled()
  })

  it("does not start a Gazelle-only run from a stale OPS/RED-only pick", async () => {
    // Saved before both Gazelle keys existed; the picker hides OPS/RED now, so nothing is left to show or store.
    Object.assign(mocks.settings, { gazelleEnabled: true, orpheusApiKey: "ops", redactedApiKey: "red" })
    mocks.indexers = [
      { id: 1, indexer_id: "orpheus", name: "OPS", base_url: "https://orpheus.example.invalid", enabled: true },
      { id: 2, indexer_id: "redacted", name: "RED", base_url: "https://redacted.example.invalid", enabled: true },
      { id: 3, indexer_id: "other", name: "Other", base_url: "https://other.example.invalid", enabled: true },
    ]
    mocks.searchSettings.indexerIds = [1, 2]
    mocks.startRun.mockResolvedValue({ id: 1 })
    renderTab()

    expect(await screen.findByText("scan.indexers.helpAllEnabledNonOpsRedQueried")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "scan.startRun" }))

    await waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
    expect(mocks.startRun.mock.calls[0][0]).toMatchObject({ indexerIds: [], disableTorznab: false })
  })
})
