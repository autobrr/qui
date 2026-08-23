/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, act, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { useBuiltinThemes, applyBuiltinThemesPayload } from "./useBuiltinThemes"
import { getThemeById, themes } from "@/config/themes"
import type { BuiltinTheme } from "@/types"

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
  mockApi: { getBuiltinThemes: vi.fn<(signal?: AbortSignal) => Promise<{ themes: BuiltinTheme[] }>>() },
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

  it("trusts the server premium flag over the CSS header", () => {
    // Directory-classified premium theme: full CSS (licensed), no @premium
    // header. The server flag is the only premium signal.
    applyBuiltinThemesPayload({
      themes: [{ id: "dirpremium", name: "Dirpremium", premium: true, css: TEST_CSS }],
    })

    const theme = getThemeById("dirpremium")
    expect(theme?.isPremium).toBe(true)
    expect(theme?.locked).toBeUndefined()
  })

  it("re-applies the stored selection once it resolves", () => {
    localStorage.setItem("color-theme", "testfree")
    applyBuiltinThemesPayload({
      themes: [{ id: "testfree", name: "Testfree", premium: false, css: TEST_CSS }],
    })

    // System change: hydration must never be pushed to the server.
    expect(mockSetTheme).toHaveBeenCalledWith("testfree", undefined, undefined, true)
  })

  it("re-applies a stored selection that resolved to a locked stub without rewriting it", () => {
    localStorage.setItem("color-theme", "locked")
    // The server payload always contains the default theme (pinned server-side).
    const minimalCss = TEST_CSS.replace("@name: Testfree", "@name: Minimal")
    applyBuiltinThemesPayload({
      themes: [
        { id: "minimal", name: "Minimal", premium: false, css: minimalCss },
        { id: "locked", name: "Locked", premium: true, preview: { light: {}, dark: {} } },
      ],
    })

    // setTheme's locked fallback paints the default itself, without touching
    // the stored selection, so the payload apply passes the stored id through.
    expect(mockSetTheme).toHaveBeenCalledWith("locked", undefined, undefined, true)
    expect(localStorage.getItem("color-theme")).toBe("locked")
  })

  it("applies the fetched default on a fresh profile with no stored selection", () => {
    const minimalCss = TEST_CSS.replace("@name: Testfree", "@name: Minimal")
    applyBuiltinThemesPayload({
      themes: [{ id: "minimal", name: "Minimal", premium: false, css: minimalCss }],
    })

    // The boot paint used the bundled fallback; the fetched default is the
    // authority and must repaint, as a system change.
    expect(mockSetTheme).toHaveBeenCalledWith("minimal", undefined, undefined, true)
    // A system restore, not a selection: setTheme stores the id itself.
  })

  it("leaves an unresolvable stored selection alone", () => {
    localStorage.setItem("color-theme", "custom:missing")
    applyBuiltinThemesPayload({
      themes: [{ id: "testfree", name: "Testfree", premium: false, css: TEST_CSS }],
    })

    expect(mockSetTheme).not.toHaveBeenCalled()
  })
})

describe("useBuiltinThemes", () => {
  it("does not register a catalog response after the query is canceled", async () => {
    let resolveRequest: ((payload: { themes: BuiltinTheme[] }) => void) | undefined
    mockApi.getBuiltinThemes.mockImplementation((signal?: AbortSignal) => new Promise((resolve, reject) => {
      resolveRequest = resolve
      signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true })
    }))

    const { unmount } = renderHook(() => useBuiltinThemes(), { wrapper })
    await waitFor(() => expect(resolveRequest).toBeTypeOf("function"))

    unmount()
    await act(async () => {
      resolveRequest?.({
        themes: [{ id: "stale", name: "Stale", premium: false, css: TEST_CSS }],
      })
      await Promise.resolve()
    })

    expect(getThemeById("stale")).toBeUndefined()
  })

  it("registers themes before observers render the committed data", async () => {
    mockApi.getBuiltinThemes.mockResolvedValue({
      themes: [{ id: "testfree", name: "Testfree", premium: false, css: TEST_CSS }],
    })

    const seenOnSuccess: boolean[] = []
    const { result } = renderHook(() => {
      const query = useBuiltinThemes()
      if (query.isSuccess) {
        seenOnSuccess.push(getThemeById("testfree") !== undefined)
      }
      return query
    }, { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    // The very first isSuccess render must already see the registered theme;
    // an effect-based registrar leaves it reading the pre-registration array.
    expect(seenOnSuccess[0]).toBe(true)
  })

  it("reports error state and keeps the fallback registry when the fetch fails", async () => {
    mockApi.getBuiltinThemes.mockRejectedValue(new Error("boom"))

    const { result } = renderHook(() => useBuiltinThemes(), { wrapper })
    // The hook retries once with backoff before settling into error state.
    await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 })

    expect(result.current.isSuccess).toBe(false)
    expect(themes.length).toBeGreaterThan(0)
  })
})
