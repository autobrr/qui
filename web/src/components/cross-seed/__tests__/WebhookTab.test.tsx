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
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    getInstances: () => Promise.resolve([{ id: 1, name: "main", isActive: true }]),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { WebhookTab } from "../WebhookTab"

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
  return await screen.findByRole("button", { name: "rules.saveChanges" })
}

describe("WebhookTab save", () => {
  it("the webhook card sends its filters, tags, and auto-resume only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const save = await renderTab()

    fireEvent.click(screen.getByRole("switch", { name: "sourceCard.autoResume" }))
    fireEvent.click(save)

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
})
