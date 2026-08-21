/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, act, cleanup } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useThemeSettingsSync } from "./useThemeSettingsSync"

const { mockApi } = vi.hoisted(() => {
  const builtinThemesResponse = Promise.resolve({ themes: [] })
  return {
    mockApi: {
      getBuiltinThemes: vi.fn(() => builtinThemesResponse),
      getThemeSettings: vi.fn(() => Promise.resolve(undefined)),
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

  it("skips system-driven changes and duplicate payloads", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "auto", isSystemChange: true })
    expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    expect(mockApi.updateThemeSettings).toHaveBeenCalledTimes(1)
  })
})
