/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, renderHook, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { ThemeSlots } from "@/types"

// The visit starts from the address at page load, so it is set before the modules load.
vi.hoisted(() => window.history.replaceState(null, "", "/?theme=default"))

const { mockApi } = vi.hoisted(() => ({
  mockApi: {
    getBuiltinThemes: vi.fn(() => Promise.resolve({ themes: [] })),
    getThemeSettings: vi.fn<() => Promise<ThemeSlots>>(() => Promise.resolve({})),
    updateThemeSettings: vi.fn(() => Promise.resolve({ themeId: "minimal", mode: "dark" })),
    deleteMobileThemeSettings: vi.fn(() => Promise.resolve()),
  },
}))
vi.mock("@/lib/api", () => ({ api: mockApi }))

const mockSetTheme = vi.hoisted(() => vi.fn(() => Promise.resolve()))
vi.mock("@/utils/theme", () => ({
  setTheme: mockSetTheme,
  getCurrentTheme: () => ({ id: "minimal" }),
  getCurrentThemeMode: () => "dark",
}))
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: vi.fn() }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))
vi.mock("@/components/ui/field-help", () => ({ FieldHelp: () => null }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

import { ThemeLayoutSettings } from "@/components/themes/ThemeLayoutSettings"
import { themes } from "@/config/themes"
import { useThemeSettingsSync } from "./useThemeSettingsSync"

function withSlots(slots: ThemeSlots) {
  mockApi.getThemeSettings.mockResolvedValue(slots)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(["theme-settings"], slots)
  return ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
  themes.splice(1, themes.length - 1)
})

describe("theme slots during a default theme visit", () => {
  it("neither pulls nor pushes a slot", () => {
    themes.push({ id: "free-theme", name: "Free", cssVars: { light: {}, dark: {} } })
    localStorage.setItem("color-theme", "custom:broken")
    renderHook(() => useThemeSettingsSync(), { wrapper: withSlots({ default: { themeId: "free-theme", mode: "dark" } }) })

    act(() => {
      window.dispatchEvent(new CustomEvent("themechange", { detail: { theme: { id: "minimal" }, mode: "dark", isSystemChange: false } }))
    })

    expect(mockSetTheme).not.toHaveBeenCalled()
    expect(localStorage.getItem("color-theme")).toBe("custom:broken")
    expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()
  })

  // regression: #4124
  it("keeps the switch for a separate mobile theme off, so no slot is written", async () => {
    const Wrapper = withSlots({ default: { themeId: "custom:broken", mode: "dark" } })
    render(<Wrapper><ThemeLayoutSettings /></Wrapper>)

    const split = screen.getByRole("switch", { name: "themes.layout.mobileSplit" })
    await waitFor(() => expect(mockApi.getThemeSettings).toHaveBeenCalled())
    expect(split.hasAttribute("disabled")).toBe(true)
  })
})
