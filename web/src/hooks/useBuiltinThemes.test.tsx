/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useBuiltinThemes, applyBuiltinThemesPayload } from "./useBuiltinThemes"
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
vi.mock("@/utils/theme", () => ({ setTheme: mockSetTheme }))

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

describe("applyBuiltinThemesPayload", () => {
  it("registers parsed themes and locked premium stubs", () => {
    applyBuiltinThemesPayload({
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

    const free = getThemeById("testfree")
    expect(free?.name).toBe("Testfree")
    expect(free?.isPremium).toBe(false)
    expect(free?.cssVars.dark["--primary"]).toBe("darkred")

    const locked = getThemeById("locked")
    expect(locked?.isPremium).toBe(true)
    expect(locked?.locked).toBe(true)
    expect(locked?.cssVars.light["--primary"]).toBe("gold")
  })

  it("re-applies the stored selection once it resolves", () => {
    localStorage.setItem("color-theme", "testfree")
    applyBuiltinThemesPayload({
      themes: [{ id: "testfree", name: "Testfree", premium: false, css: TEST_CSS }],
    })

    expect(mockSetTheme).toHaveBeenCalledWith("testfree")
  })

  it("downgrades a stored selection that resolved to a locked stub", () => {
    localStorage.setItem("color-theme", "locked")
    // The server payload always contains the default theme (pinned server-side).
    const minimalCss = TEST_CSS.replace("@name: Testfree", "@name: Minimal")
    applyBuiltinThemesPayload({
      themes: [
        { id: "minimal", name: "Minimal", premium: false, css: minimalCss },
        { id: "locked", name: "Locked", premium: true, preview: { light: {}, dark: {} } },
      ],
    })

    expect(mockSetTheme).toHaveBeenCalledWith("minimal")
  })
})

describe("useBuiltinThemes", () => {
  it("reports error state and keeps the fallback registry when the fetch fails", async () => {
    mockApi.getBuiltinThemes.mockRejectedValue(new Error("boom"))

    const { result } = renderHook(() => useBuiltinThemes(), { wrapper })
    // The hook retries once with backoff before settling into error state.
    await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 })

    expect(result.current.isSuccess).toBe(false)
    expect(themes.length).toBeGreaterThan(0)
  })
})
