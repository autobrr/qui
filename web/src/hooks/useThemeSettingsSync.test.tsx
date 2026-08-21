/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, act, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useThemeSettingsSync } from "./useThemeSettingsSync"
import type { ThemeSettings } from "@/types"

const { mockApi } = vi.hoisted(() => {
  const builtinThemesResponse = Promise.resolve({ themes: [] })
  return {
    mockApi: {
      getBuiltinThemes: vi.fn(() => builtinThemesResponse),
      getThemeSettings: vi.fn<() => Promise<ThemeSettings | undefined>>(() => Promise.resolve(undefined)),
      updateThemeSettings: vi.fn(() => Promise.resolve({ themeId: "minimal", mode: "dark" })),
    },
  }
})

vi.mock("@/lib/api", () => ({ api: mockApi }))

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function dispatchThemeChange(detail: object) {
  act(() => {
    window.dispatchEvent(new CustomEvent("themechange", { detail }))
  })
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
})

describe("useThemeSettingsSync", () => {
  it("pushes local theme changes to the server", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false, variant: "blue" })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "minimal",
      mode: "dark",
      variation: "blue",
    })
  })

  it("pushes the stored selection, not the applied fallback theme", () => {
    // Mode toggle during the locked-premium fallback: sync the new mode
    // without replacing the stored selection on the server.
    localStorage.setItem("color-theme", "locked-premium")
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "locked-premium",
      mode: "dark",
    })
  })

  it("mirrors a pulled unresolvable selection so a mode change keeps it", async () => {
    // Fresh browser, server selection is a premium theme this client cannot
    // resolve: the pull must persist the id locally, or the next mode toggle
    // would push the applied fallback id over the server selection.
    mockApi.getThemeSettings.mockResolvedValue({ themeId: "locked-premium", mode: "light" })
    renderHook(() => useThemeSettingsSync(), { wrapper })

    await waitFor(() => {
      expect(localStorage.getItem("color-theme")).toBe("locked-premium")
    })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "locked-premium",
      mode: "dark",
    })
  })

  it("skips system-driven changes and duplicate payloads", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "auto", isSystemChange: true })
    expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    expect(mockApi.updateThemeSettings).toHaveBeenCalledTimes(1)
  })
})
