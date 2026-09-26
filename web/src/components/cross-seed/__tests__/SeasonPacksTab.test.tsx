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
    seasonPackEnabled: true,
    seasonPackAutomationEnabled: false,
    seasonPackSkipRepackCompare: true,
    seasonPackSimplifyHdrCompare: false,
    seasonPackSimplifyWebCompare: false,
    seasonPackSkipYearCompare: false,
    seasonPackCoverageThreshold: 0.75,
    seasonPackTags: ["cross-seed", "season-pack"],
    seasonPackCategory: "tv",
    seasonPackCategoryRules: [],
    seasonPackTvdbApiKey: "key",
    seasonPackTvdbPin: "",
    rssAutomationTags: ["rss"],
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
vi.mock("@/components/cross-seed/SeasonPackCategoryRulesEditor", () => ({
  SeasonPackCategoryRulesEditor: () => <div data-testid="rules-editor" />,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: () => Promise.resolve([]),
    listSeasonPackRuns: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import type { CrossSeedAutomationSettings } from "@/types"
import { SeasonPacksTab } from "../SeasonPacksTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("SeasonPacksTab save", () => {
  it("sends every season pack field and nothing else", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={client}>
        <SeasonPacksTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
      </QueryClientProvider>
    )

    const threshold = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#season-pack-threshold")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(threshold, { target: { value: "90" } })
    fireEvent.click(screen.getByRole("switch", { name: "rules.seasonPack.enableAutomation" }))
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      seasonPackEnabled: true,
      seasonPackAutomationEnabled: true,
      seasonPackSkipRepackCompare: true,
      seasonPackSimplifyHdrCompare: false,
      seasonPackSimplifyWebCompare: false,
      seasonPackSkipYearCompare: false,
      seasonPackCoverageThreshold: 0.9,
      seasonPackTags: ["cross-seed", "season-pack"],
      seasonPackCategory: "tv",
      seasonPackCategoryRules: [],
      seasonPackTvdbApiKey: undefined,
      seasonPackTvdbPin: undefined,
    })
  })
})
