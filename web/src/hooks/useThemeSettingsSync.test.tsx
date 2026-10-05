/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook, act, cleanup, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { setThemeLocalOnly, useThemeSettingsSync } from "./useThemeSettingsSync"
import { setStoredVariation } from "@/hooks/usePersistedThemeVariation"
import { themes } from "@/config/themes"
import type { ThemeSlots } from "@/types"

const { mockApi } = vi.hoisted(() => {
  const builtinThemesResponse = Promise.resolve({ themes: [] })
  return {
    mockApi: {
      getBuiltinThemes: vi.fn(() => builtinThemesResponse),
      getThemeSettings: vi.fn<() => Promise<ThemeSlots>>(() => Promise.resolve({})),
      updateThemeSettings: vi.fn(() => Promise.resolve({ themeId: "minimal", mode: "dark" })),
    },
  }
})

vi.mock("@/lib/api", () => ({ api: mockApi }))

// The real setTheme needs a full theme registry; the pull tests assert the
// applied selection through this mock.
const mockSetTheme = vi.hoisted(() => vi.fn(() => Promise.resolve()))
vi.mock("@/utils/theme", () => ({ setTheme: mockSetTheme }))

// The real hook needs a SyncStreamProvider; the registration itself is the
// provider's concern, so it is mocked and only the enabled flag is asserted.
const mockUseActivityStream = vi.hoisted(() => vi.fn())
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: mockUseActivityStream }))

// jsdom has no matchMedia. The stub answers the mobile query from viewportWidth
// and lets a test fire a breakpoint crossing.
let viewportWidth = 1024
const mediaListeners = new Set<() => void>()
window.matchMedia = (query: string) => ({
  matches: query === "(max-width: 767px)" && viewportWidth < 768,
  media: query,
  onchange: null,
  addEventListener: (_: string, listener: () => void) => mediaListeners.add(listener),
  removeEventListener: (_: string, listener: () => void) => mediaListeners.delete(listener),
  addListener: () => {},
  removeListener: () => {},
  dispatchEvent: () => false,
}) as unknown as MediaQueryList

function resizeTo(width: number) {
  act(() => {
    viewportWidth = width
    mediaListeners.forEach((listener) => listener())
  })
}

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
  viewportWidth = 1024
  vi.clearAllMocks()
  localStorage.clear()
  // Reset the module-level registry to just the bundled fallback.
  themes.splice(1, themes.length - 1)
})

// A wrapper that exposes its QueryClient so a test can spy on invalidation.
function makeWrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidateSpy = vi.spyOn(client, "invalidateQueries")
  function spiedWrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
  }
  return { spiedWrapper, invalidateSpy }
}

describe("useThemeSettingsSync", () => {
  it("gates the activity stream on a cached authenticated user", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })
    expect(mockUseActivityStream).toHaveBeenLastCalledWith(false)

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(["auth", "user"], { username: "admin" })
    renderHook(() => useThemeSettingsSync(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    })
    expect(mockUseActivityStream).toHaveBeenLastCalledWith(true)
  })

  it("refetches and applies settings when authentication succeeds", async () => {
    themes.push({ id: "free-theme", name: "Free", cssVars: { light: {}, dark: {} } })
    mockApi.getThemeSettings
      .mockRejectedValueOnce(new Error("pre-auth request failed"))
      .mockResolvedValueOnce({ default: { themeId: "free-theme", mode: "dark" } })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    renderHook(() => useThemeSettingsSync(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    })
    await waitFor(() => {
      expect(client.getQueryState(["theme-settings"])?.status).toBe("error")
    })

    act(() => client.setQueryData(["auth", "user"], { username: "admin" }))

    await waitFor(() => {
      expect(localStorage.getItem("color-theme")).toBe("free-theme")
    })
  })

  it("pushes local theme changes to the server", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false, variant: "blue" })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "minimal",
      mode: "dark",
      variation: "blue",
    }, "default")
  })

  it("pushes the stored selection and variation, not the applied fallback theme", () => {
    // Mode toggle during the locked-premium fallback: sync the new mode
    // without replacing the stored selection or variation on the server.
    localStorage.setItem("color-theme", "locked-premium")
    setStoredVariation("locked-premium", "purple")
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "locked-premium",
      mode: "dark",
      variation: "purple",
    }, "default")
  })

  it("mirrors a pulled unresolvable selection so a mode change keeps it", async () => {
    // Fresh browser, server selection is a premium theme this client cannot
    // resolve: the pull must persist the id locally, or the next mode toggle
    // would push the applied fallback id over the server selection.
    mockApi.getThemeSettings.mockResolvedValue({ default: { themeId: "locked-premium", mode: "light" } })
    renderHook(() => useThemeSettingsSync(), { wrapper })

    await waitFor(() => {
      expect(localStorage.getItem("color-theme")).toBe("locked-premium")
    })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })

    expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({
      themeId: "locked-premium",
      mode: "dark",
    }, "default")
  })

  it("refetches the catalog when the server selection resolves to a locked stub", async () => {
    // The stale catalog only stubs the new selection, but the server serves
    // the selected theme's CSS even pre-auth: the pull must force a refetch
    // instead of sitting on the default until the hourly refresh.
    themes.push({ id: "locked-premium", name: "Locked", locked: true, cssVars: { light: {}, dark: {} } })
    mockApi.getThemeSettings.mockResolvedValue({ default: { themeId: "locked-premium", mode: "auto" } })
    const { spiedWrapper, invalidateSpy } = makeWrapper()

    renderHook(() => useThemeSettingsSync(), { wrapper: spiedWrapper })

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["builtin-themes"] })
    })
  })

  it("does not refetch the catalog for a selection that resolves with CSS", async () => {
    themes.push({ id: "free-theme", name: "Free", cssVars: { light: {}, dark: {} } })
    mockApi.getThemeSettings.mockResolvedValue({ default: { themeId: "free-theme", mode: "auto" } })
    const { spiedWrapper, invalidateSpy } = makeWrapper()

    renderHook(() => useThemeSettingsSync(), { wrapper: spiedWrapper })

    await waitFor(() => {
      expect(localStorage.getItem("color-theme")).toBe("free-theme")
    })
    expect(invalidateSpy).not.toHaveBeenCalledWith({ queryKey: ["builtin-themes"] })
  })

  it("skips system-driven changes and duplicate payloads", () => {
    renderHook(() => useThemeSettingsSync(), { wrapper })

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "auto", isSystemChange: true })
    expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()

    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
    expect(mockApi.updateThemeSettings).toHaveBeenCalledTimes(1)
  })

  describe("layout slots", () => {
    const both: ThemeSlots = {
      default: { themeId: "desk-theme", mode: "auto" },
      mobile: { themeId: "phone-theme", mode: "dark" },
    }

    function registerSlotThemes() {
      themes.push(
        { id: "desk-theme", name: "Desk", cssVars: { light: {}, dark: {} } },
        { id: "phone-theme", name: "Phone", cssVars: { light: {}, dark: {} } }
      )
    }

    it.each([375, 1024])("uses the default slot at %ipx without a mobile slot", async (width) => {
      registerSlotThemes()
      viewportWidth = width
      mockApi.getThemeSettings.mockResolvedValue({ default: both.default })
      renderHook(() => useThemeSettingsSync(), { wrapper })

      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenCalledWith("desk-theme", "auto", undefined, true)
      })
      dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })
      expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({ themeId: "desk-theme", mode: "dark" }, "default")
    })

    it("applies and pushes the mobile slot on a narrow viewport", async () => {
      registerSlotThemes()
      viewportWidth = 375
      mockApi.getThemeSettings.mockResolvedValue(both)
      renderHook(() => useThemeSettingsSync(), { wrapper })

      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenCalledWith("phone-theme", "dark", undefined, true)
      })
      expect(mockSetTheme).not.toHaveBeenCalledWith("desk-theme", expect.anything(), undefined, true)

      dispatchThemeChange({ theme: { id: "phone-theme" }, mode: "light", isSystemChange: false })
      expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({ themeId: "phone-theme", mode: "light" }, "mobile")
    })

    it("applies the default slot on a wide viewport", async () => {
      registerSlotThemes()
      mockApi.getThemeSettings.mockResolvedValue(both)
      renderHook(() => useThemeSettingsSync(), { wrapper })

      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenCalledWith("desk-theme", "auto", undefined, true)
      })
      expect(mockSetTheme).not.toHaveBeenCalledWith("phone-theme", expect.anything(), undefined, true)
    })

    it("applies the other slot without a PUT when the viewport crosses the breakpoint", async () => {
      registerSlotThemes()
      mockApi.getThemeSettings.mockResolvedValue(both)
      renderHook(() => useThemeSettingsSync(), { wrapper })
      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenCalledWith("desk-theme", "auto", undefined, true)
      })

      resizeTo(375)
      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenLastCalledWith("phone-theme", "dark", undefined, true)
      })
      // The applied pull fires a system themechange, which must not push.
      dispatchThemeChange({ theme: { id: "phone-theme" }, mode: "dark", isSystemChange: true })

      resizeTo(1024)
      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenLastCalledWith("desk-theme", "auto", undefined, true)
      })
      expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()
    })

    it("neither applies nor pushes while this browser keeps its theme local", async () => {
      registerSlotThemes()
      setThemeLocalOnly(true)
      mockApi.getThemeSettings.mockResolvedValue(both)
      renderHook(() => useThemeSettingsSync(), { wrapper })

      await waitFor(() => {
        expect(mockApi.getThemeSettings).toHaveBeenCalled()
      })
      dispatchThemeChange({ theme: { id: "minimal" }, mode: "dark", isSystemChange: false })

      expect(mockSetTheme).not.toHaveBeenCalledWith("desk-theme", expect.anything(), undefined, true)
      expect(mockSetTheme).not.toHaveBeenCalledWith("phone-theme", expect.anything(), undefined, true)
      expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()
    })

    it("applies the server slot when the local flag is cleared", async () => {
      registerSlotThemes()
      setThemeLocalOnly(true)
      mockApi.getThemeSettings.mockResolvedValue(both)
      renderHook(() => useThemeSettingsSync(), { wrapper })
      await waitFor(() => {
        expect(mockApi.getThemeSettings).toHaveBeenCalled()
      })

      act(() => setThemeLocalOnly(false))

      await waitFor(() => {
        expect(mockSetTheme).toHaveBeenCalledWith("desk-theme", "auto", undefined, true)
      })
      expect(mockApi.updateThemeSettings).not.toHaveBeenCalled()
    })

    it("refetches the catalog when the mobile slot resolves to a locked stub", async () => {
      themes.push({ id: "locked-premium", name: "Locked", locked: true, cssVars: { light: {}, dark: {} } })
      viewportWidth = 375
      mockApi.getThemeSettings.mockResolvedValue({
        default: { themeId: "minimal", mode: "auto" },
        mobile: { themeId: "locked-premium", mode: "dark" },
      })
      const { spiedWrapper, invalidateSpy } = makeWrapper()

      renderHook(() => useThemeSettingsSync(), { wrapper: spiedWrapper })

      await waitFor(() => {
        expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["builtin-themes"] })
      })
      expect(localStorage.getItem("color-theme")).toBe("locked-premium")
    })

    it("keeps the stored selection and skips duplicates in the mobile slot", async () => {
      viewportWidth = 375
      mockApi.getThemeSettings.mockResolvedValue({
        default: { themeId: "desk-theme", mode: "auto" },
        mobile: { themeId: "locked-premium", mode: "dark" },
      })
      renderHook(() => useThemeSettingsSync(), { wrapper })
      await waitFor(() => {
        expect(localStorage.getItem("color-theme")).toBe("locked-premium")
      })

      dispatchThemeChange({ theme: { id: "minimal" }, mode: "auto", isSystemChange: true })
      dispatchThemeChange({ theme: { id: "minimal" }, mode: "light", isSystemChange: false })
      dispatchThemeChange({ theme: { id: "minimal" }, mode: "light", isSystemChange: false })

      expect(mockApi.updateThemeSettings).toHaveBeenCalledExactlyOnceWith({ themeId: "locked-premium", mode: "light" }, "mobile")
    })
  })
})
