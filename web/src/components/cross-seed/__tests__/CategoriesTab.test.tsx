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
    // Flags that disagree: custom wins, and the patch sends exactly one true flag.
    useCustomCategory: true,
    useCategoryFromIndexer: true,
    useCrossCategoryAffix: true,
    categoryAffixMode: "suffix",
    categoryAffix: ".cross",
    customCategory: "seeds",
    inheritSourceTags: false,
    skipRecheck: false,
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
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import type { CrossSeedAutomationSettings } from "@/types"
import { CategoriesTab } from "../CategoriesTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  mocks.settings.customCategory = "seeds"
})

async function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <CategoriesTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
    </QueryClientProvider>
  )
  return screen.findByRole("button", { name: "rules.saveChanges" })
}

describe("CategoriesTab save", () => {
  it("sends one category mode and the inherit-tags switch only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const save = await renderTab()

    fireEvent.click(screen.getByRole("switch", { name: "rules.tagging.inheritSourceTags" }))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      useCustomCategory: true,
      useCategoryFromIndexer: false,
      useCrossCategoryAffix: false,
      categoryAffixMode: "suffix",
      categoryAffix: ".cross",
      customCategory: "seeds",
      inheritSourceTags: true,
    })
  })

  it("rejects custom category mode without a category", async () => {
    mocks.settings.customCategory = ""
    const save = await renderTab()
    fireEvent.click(save)

    expect(screen.getByText("toast.customCategoryRequired")).toBeTruthy()
    expect(mocks.patchSettings).not.toHaveBeenCalled()
  })
})
