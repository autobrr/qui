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
    pooledPartialCompletionEnabled: false,
    autoResumeMaxDownloadMb: 50,
    runExternalProgramId: null,
    skipRecheck: false,
    inheritSourceTags: true,
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
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getCrossSeedSettings: () => Promise.resolve(mocks.settings),
    listExternalPrograms: () => Promise.resolve([]),
    getTorznabSearchCacheStats: () => Promise.resolve(null),
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import { AfterInjectionTab } from "./AfterInjectionTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("AfterInjectionTab save", () => {
  it("sends the pooled switch, the byte budget and the external program only", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <AfterInjectionTab />
      </QueryClientProvider>
    )
    const save = await screen.findByRole("button", { name: "rules.saveChanges" })

    fireEvent.change(screen.getByLabelText("rules.postInjection.maxAutoResumeDownload"), { target: { value: "200" } })
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      pooledPartialCompletionEnabled: false,
      autoResumeMaxDownloadMb: 200,
      runExternalProgramId: null,
    })
  })
})
