/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  settings: {
    categoryMappingRules: [],
    findIndividualEpisodes: false,
    rescueTitleMismatches: false,
    skipRecheck: false,
    skipPieceBoundarySafetyCheck: true,
    // Flags that disagree: custom wins, and the patch sends exactly one true flag.
    useCustomCategory: true,
    useCategoryFromIndexer: true,
    useCrossCategoryAffix: true,
    categoryAffixMode: "suffix",
    categoryAffix: ".cross",
    customCategory: "seeds",
    inheritSourceTags: false,
    pooledPartialCompletionEnabled: false,
    autoResumeMaxDownloadMb: 50,
    runExternalProgramId: null,
    rssAutomationTags: ["rss"],
    seasonPackEnabled: true,
  },
  patchSettings: vi.fn(),
  instances: { instances: [], updateInstance: vi.fn(), isUpdating: false },
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
vi.mock("@/components/crossseed/CategoryMappingRulesEditor", () => ({
  CategoryMappingRulesEditor: () => <div data-testid="mapping-editor" />,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    getInstances: () => Promise.resolve([]),
    listExternalPrograms: () => Promise.resolve([]),
    getTorznabSearchCacheStats: () => Promise.resolve(null),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { RulesTab } from "./RulesTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

async function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <RulesTab />
    </QueryClientProvider>
  )
  return waitFor(() => screen.getByRole("button", { name: "rules.saveGlobalSettings" }))
}

describe("RulesTab save", () => {
  it("sends the global fields with one category mode and nothing from a source card", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const save = await renderTab()

    fireEvent.click(screen.getByRole("switch", { name: "rules.safety.skipRecheck" }))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      categoryMappingRules: [],
      findIndividualEpisodes: false,
      rescueTitleMismatches: false,
      skipRecheck: true,
      skipPieceBoundarySafetyCheck: true,
      useCustomCategory: true,
      useCategoryFromIndexer: false,
      useCrossCategoryAffix: false,
      categoryAffixMode: "suffix",
      categoryAffix: ".cross",
      customCategory: "seeds",
      inheritSourceTags: false,
      pooledPartialCompletionEnabled: false,
      autoResumeMaxDownloadMb: 50,
      runExternalProgramId: null,
    })
  })

  it("rejects custom category mode without a category", async () => {
    mocks.settings.customCategory = ""
    const save = await renderTab()
    fireEvent.click(save)

    expect(screen.getByText("toast.customCategoryRequired")).toBeTruthy()
    expect(mocks.patchSettings).not.toHaveBeenCalled()
    mocks.settings.customCategory = "seeds"
  })
})
