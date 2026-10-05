/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { ThemeSlots } from "@/types"

const { mockApi } = vi.hoisted(() => ({
  mockApi: {
    getThemeSettings: vi.fn<() => Promise<ThemeSlots>>(),
    updateThemeSettings: vi.fn(() => Promise.resolve({ themeId: "nord", mode: "dark" })),
    deleteMobileThemeSettings: vi.fn(() => Promise.resolve()),
  },
}))
vi.mock("@/lib/api", () => ({ api: mockApi }))

vi.mock("@/utils/theme", () => ({
  getCurrentTheme: () => ({ id: "nord" }),
  getCurrentThemeMode: () => "dark",
}))

vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))

vi.mock("@/components/ui/field-help", () => ({ FieldHelp: () => null }))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { ThemeLayoutSettings } from "@/components/themes/ThemeLayoutSettings"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
})

function renderSettings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><ThemeLayoutSettings /></QueryClientProvider>)
}

describe("ThemeLayoutSettings", () => {
  it("stores the current theme in the default slot when the split turns on without one", async () => {
    mockApi.getThemeSettings.mockResolvedValue({})
    renderSettings()

    const split = screen.getByRole("switch", { name: "themes.layout.mobileSplit" })
    await waitFor(() => expect(split.hasAttribute("disabled")).toBe(false))
    await act(async () => split.click())

    // Without the default slot, turning the split off again leaves no theme to pull.
    expect(mockApi.updateThemeSettings.mock.calls).toEqual([
      [{ themeId: "nord", mode: "dark" }, "default"],
      [{ themeId: "nord", mode: "dark" }, "mobile"],
    ])
  })

  it("leaves an existing default slot alone when the split turns on", async () => {
    mockApi.getThemeSettings.mockResolvedValue({ default: { themeId: "minimal", mode: "light" } })
    renderSettings()

    const split = screen.getByRole("switch", { name: "themes.layout.mobileSplit" })
    await waitFor(() => expect(split.hasAttribute("disabled")).toBe(false))
    await act(async () => split.click())

    expect(mockApi.updateThemeSettings.mock.calls).toEqual([
      [{ themeId: "nord", mode: "dark" }, "mobile"],
    ])
  })
})
