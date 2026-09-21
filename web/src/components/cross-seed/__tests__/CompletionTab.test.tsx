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
    completionSearchTags: ["cross-seed"],
    skipAutoResumeCompletion: false,
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
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { CompletionTab } from "../CompletionTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("CompletionTab save", () => {
  it("sends the completion tags and auto-resume only, with the per-instance overview below", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <CompletionTab />
      </QueryClientProvider>
    )
    const save = await screen.findByRole("button", { name: "rules.saveChanges" })
    expect(screen.getByTestId("completion-overview")).toBeTruthy()

    fireEvent.click(screen.getByRole("switch", { name: "sourceCard.autoResume" }))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      completionSearchTags: ["cross-seed"],
      skipAutoResumeCompletion: true,
    })
  })
})
