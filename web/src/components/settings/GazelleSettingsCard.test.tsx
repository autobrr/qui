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
    gazelleEnabled: false,
    redactedApiKey: "",
    orpheusApiKey: "",
    skipRecheck: true,
  },
  patchSettings: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { toast } from "sonner"
import { CROSS_SEED_SETTINGS_KEY } from "@/hooks/useCrossSeedSettings"
import { GazelleSettingsCard } from "./GazelleSettingsCard"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("GazelleSettingsCard save", () => {
  it("sends the Gazelle switch and keys only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <GazelleSettingsCard />
      </QueryClientProvider>
    )
    const save = await screen.findByRole("button", { name: "gazelle.save" })

    fireEvent.click(screen.getByRole("switch", { name: "gazelle.enableMatching" }))
    fireEvent.change(screen.getByLabelText("gazelle.redactedApiKey"), { target: { value: "red-key" } })
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      gazelleEnabled: true,
      redactedApiKey: "red-key",
      orpheusApiKey: undefined,
    })
  })

  it("replaces a saved key with the placeholder from the response", async () => {
    mocks.patchSettings.mockResolvedValue({ ...mocks.settings, gazelleEnabled: true, redactedApiKey: "<redacted>" })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <GazelleSettingsCard />
      </QueryClientProvider>
    )
    const save = await screen.findByRole("button", { name: "gazelle.save" })

    fireEvent.click(screen.getByRole("switch", { name: "gazelle.enableMatching" }))
    fireEvent.change(screen.getByLabelText("gazelle.redactedApiKey"), { target: { value: "red-key" } })
    fireEvent.click(save)

    await waitFor(() => expect(screen.getByLabelText("gazelle.redactedApiKey")).toHaveProperty("value", "<redacted>"))
  })

  it("warns with the server text when qui could not check the key", async () => {
    mocks.patchSettings.mockResolvedValue({ ...mocks.settings, warning: "qui could not check the RED API key." })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <GazelleSettingsCard />
      </QueryClientProvider>
    )
    fireEvent.click(await screen.findByRole("button", { name: "gazelle.save" }))

    await waitFor(() => expect(toast.warning).toHaveBeenCalledWith("toast.settingsSavedKeyUnchecked", { description: "qui could not check the RED API key." }))
    expect(toast.success).not.toHaveBeenCalled()
    expect(client.getQueryData(CROSS_SEED_SETTINGS_KEY)).not.toHaveProperty("warning")
  })
})
