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
    inheritSourceTags: true,
    autoResumeMaxDownloadMb: 50,
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
vi.mock("@/components/cross-seed/CategoryMappingRulesEditor", () => ({
  CategoryMappingRulesEditor: () => <div data-testid="mapping-editor" />,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import type { CrossSeedAutomationSettings } from "@/types"
import { MatchingTab } from "../MatchingTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("MatchingTab save", () => {
  it("sends the matching fields only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <MatchingTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
      </QueryClientProvider>
    )
    const save = await screen.findByRole("button", { name: "rules.saveChanges" })

    fireEvent.click(screen.getByRole("switch", { name: "rules.safety.skipRecheck" }))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      categoryMappingRules: [],
      findIndividualEpisodes: false,
      rescueTitleMismatches: false,
      skipRecheck: true,
      skipPieceBoundarySafetyCheck: true,
    })
  })
})
