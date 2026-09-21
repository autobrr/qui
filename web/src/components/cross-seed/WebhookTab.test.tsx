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
    webhookSourceCategories: [],
    webhookSourceTags: ["keep"],
    webhookSourceExcludeCategories: [],
    webhookSourceExcludeTags: [],
    webhookTags: ["cross-seed", "autobrr"],
    skipAutoResumeWebhook: false,
    completionSearchTags: ["cross-seed"],
    skipAutoResumeCompletion: false,
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
vi.mock("@/components/instances/preferences/CompletionOverview", () => ({
  CompletionOverview: () => <div data-testid="completion-overview" />,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    getInstances: () => Promise.resolve([{ id: 1, name: "main", isActive: true }]),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { WebhookTab } from "./WebhookTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

async function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <WebhookTab />
    </QueryClientProvider>
  )
  await waitFor(() => expect(screen.getAllByRole("button", { name: "rules.saveChanges" })).toHaveLength(2))
  const [webhookSave, completionSave] = screen.getAllByRole("button", { name: "rules.saveChanges" })
  return { webhookSave, completionSave }
}

describe("WebhookTab save", () => {
  it("the webhook card sends its filters, tags, and auto-resume only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const { webhookSave } = await renderTab()

    const [webhookResume] = screen.getAllByRole("switch", { name: "sourceCard.autoResume" })
    fireEvent.click(webhookResume)
    fireEvent.click(webhookSave)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      webhookSourceCategories: [],
      webhookSourceTags: ["keep"],
      webhookSourceExcludeCategories: [],
      webhookSourceExcludeTags: [],
      webhookTags: ["cross-seed", "autobrr"],
      skipAutoResumeWebhook: true,
    })
  })

  it("the completion card sends its tags and auto-resume only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const { completionSave } = await renderTab()

    const [, completionResume] = screen.getAllByRole("switch", { name: "sourceCard.autoResume" })
    fireEvent.click(completionResume)
    fireEvent.click(completionSave)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      completionSearchTags: ["cross-seed"],
      skipAutoResumeCompletion: true,
    })
  })
})
