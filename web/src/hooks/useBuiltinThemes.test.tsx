/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useBuiltinThemes } from "./useBuiltinThemes"
import { getThemeById, themes } from "@/config/themes"

const TEST_CSS = `/* @name: Testfree
 * @description: A test theme
 * @premium: false
 */

:root {
  --background: oklch(1 0 0);
  --primary: red;
}

.dark {
  --background: oklch(0 0 0);
  --primary: darkred;
}
`

const { mockApi, mockSetTheme } = vi.hoisted(() => ({
  mockApi: { getBuiltinThemes: vi.fn() },
  mockSetTheme: vi.fn(() => Promise.resolve()),
}))

vi.mock("@/lib/api", () => ({ api: mockApi }))
vi.mock("@/utils/theme", () => ({
  setTheme: mockSetTheme,
  getCurrentTheme: () => ({ id: "default" }),
}))

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
  // Reset the module-level registry to just the bundled fallback.
  themes.splice(1, themes.length - 1)
})

describe("useBuiltinThemes", () => {
  it("registers parsed themes and locked premium stubs", async () => {
    mockApi.getBuiltinThemes.mockResolvedValue({
      themes: [
        { id: "testfree", name: "Testfree", premium: false, css: TEST_CSS },
        {
          id: "locked",
          name: "Locked",
          premium: true,
          preview: { light: { "--primary": "gold" }, dark: { "--primary": "goldenrod" } },
        },
      ],
    })

    const { result } = renderHook(() => useBuiltinThemes(), { wrapper })
    await waitFor(() => expect(result.current.isReady).toBe(true))

    const free = getThemeById("testfree")
    expect(free?.name).toBe("Testfree")
    expect(free?.isPremium).toBe(false)
    expect(free?.cssVars.dark["--primary"]).toBe("darkred")

    const locked = getThemeById("locked")
    expect(locked?.isPremium).toBe(true)
    expect(locked?.cssVars.light["--primary"]).toBe("gold")
  })

  it("re-applies the stored selection once it resolves", async () => {
    localStorage.setItem("color-theme", "testfree")
    mockApi.getBuiltinThemes.mockResolvedValue({
      themes: [{ id: "testfree", name: "Testfree", premium: false, css: TEST_CSS }],
    })

    const { result } = renderHook(() => useBuiltinThemes(), { wrapper })
    await waitFor(() => expect(result.current.isReady).toBe(true))

    expect(mockSetTheme).toHaveBeenCalledWith("testfree")
  })

  it("keeps the fallback and reports error state when the fetch fails", async () => {
    mockApi.getBuiltinThemes.mockRejectedValue(new Error("boom"))

    const { result } = renderHook(() => useBuiltinThemes(), { wrapper })
    // The hook retries once with backoff before settling into error state.
    await waitFor(() => expect(result.current.isReady).toBe(true), { timeout: 5000 })

    expect(result.current.isError).toBe(true)
    expect(themes.length).toBeGreaterThan(0)
  })
})
