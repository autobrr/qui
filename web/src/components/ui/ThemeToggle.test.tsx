/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { _resetClientSettingsForTests, applyServerSettings } from "@/lib/client-settings"

const fixture = vi.hoisted(() => {
  const makeTheme = (
    id: string,
    name: string,
    options: Pick<Theme, "isPremium" | "isCustom" | "variations" | "locked"> = {}
  ): Theme => ({
    id,
    name,
    ...options,
    cssVars: {
      light: {
        "--primary": `${id}-primary`,
        "--secondary": `${id}-secondary`,
        "--accent": `${id}-accent`,
        "--variation-blue": "blue",
        "--variation-green": "green",
        "--variation-amber": "orange",
      },
      dark: {
        "--primary": `${id}-primary`,
        "--secondary": `${id}-secondary`,
        "--accent": `${id}-accent`,
        "--variation-blue": "navy",
        "--variation-green": "darkgreen",
        "--variation-amber": "orange",
      },
    },
  })

  const selectedTheme = makeTheme("selected", "Selected theme", {
    variations: ["blue", "green"],
  })
  const otherTheme = makeTheme("other", "Other theme", {
    variations: ["amber"],
  })
  // Unlicensed install: the server ships premium themes as locked stubs.
  const premiumTheme = makeTheme("nord", "Premium theme", {
    isPremium: true,
    locked: true,
  })
  // A community theme that is new to the build starts hidden.
  const communityTheme = makeTheme("gruvbox", "Community theme", {
    isPremium: true,
    locked: true,
  })
  const customTheme = makeTheme("custom:local", "Custom theme", {
    isCustom: true,
  })

  return {
    builtInThemes: [selectedTheme, premiumTheme, otherTheme, communityTheme],
    customThemes: [customTheme],
    selectedTheme,
  }
})

vi.mock("@/config/themes", () => ({
  themes: fixture.builtInThemes,
  getThemeById: (id: string) =>
    [...fixture.builtInThemes, ...fixture.customThemes].find((theme) => theme.id === id),
}))

const appearance = vi.hoisted(() => ({ dark: true }))

vi.mock("@/utils/theme", () => ({
  getCurrentThemeMode: () => "dark",
  getCurrentTheme: () => fixture.selectedTheme,
  getThemeVariation: (id?: string) => id === "other" ? "amber" : "blue",
  isDarkMode: () => appearance.dark,
  setTheme: vi.fn(),
  setThemeMode: vi.fn(),
  setThemeVariation: vi.fn(),
}))

vi.mock("@tanstack/react-router", async () => {
  const { createElement } = await import("react")
  return {
    Link: ({ to, search: _search, children, ...props }: { to: string; search?: unknown; children?: React.ReactNode }) =>
      createElement("a", { href: to, ...props }, children),
  }
})

vi.mock("@/hooks/useLicense.ts", () => ({
  useHasPremiumAccess: () => ({
    hasPremiumAccess: false,
    isLoading: false,
    isError: false,
  }),
}))

const builtinsResult = vi.hoisted(() => ({ data: undefined, isSuccess: true, isError: false }))
vi.mock("@/hooks/useBuiltinThemes", () => ({
  useBuiltinThemes: () => builtinsResult,
}))

vi.mock("@/hooks/useCustomThemes", () => ({
  useCustomThemes: () => ({ customThemes: fixture.customThemes }),
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

import { ThemeToggle } from "@/components/ui/ThemeToggle"

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  appearance.dark = true
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function openMenu(): void {
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "themeToggle.changeTheme" }),
    { button: 0, ctrlKey: false }
  )
}

function saveVisibility(visibility: Record<string, boolean>): void {
  localStorage.setItem("qui-theme-visibility", JSON.stringify(visibility))
}

function themeItem(name: string): HTMLElement {
  const item = screen.getByText(name).closest("[role=menuitem]")
  if (!(item instanceof HTMLElement)) {
    throw new Error(`Theme menu item not found: ${name}`)
  }
  return item
}

function variationColor(name: string): string {
  const swatch = screen.getByRole("menuitemradio", { name }).querySelector("span")
  if (!swatch) throw new Error(`Variation swatch not found: ${name}`)
  return swatch.style.backgroundColor
}

describe("ThemeToggle", () => {
  it("shows the complete catalog, with color buttons only for the theme in use", async () => {
    render(<ThemeToggle />, { wrapper: TooltipProvider })

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "themeToggle.changeTheme" }),
      { button: 0, ctrlKey: false }
    )

    const selected = await screen.findByText("Selected theme")
    const selectedItem = selected.closest("[role=menuitem]") as HTMLElement
    const otherItem = themeItem("Other theme")
    const premiumItem = themeItem("Premium theme")
    const customItem = themeItem("Custom theme")
    const items = screen.getAllByRole("menuitem")

    expect(items.indexOf(selectedItem)).toBeLessThan(items.indexOf(otherItem))
    expect(items.indexOf(otherItem)).toBeLessThan(items.indexOf(premiumItem))
    expect(items.indexOf(premiumItem)).toBeLessThan(items.indexOf(customItem))
    expect(premiumItem.hasAttribute("data-disabled")).toBe(true)

    // Menu items of their own, so the arrow keys reach them.
    expect(screen.getByRole("menuitemradio", { name: "blue" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("menuitemradio", { name: "green" }).getAttribute("aria-checked")).toBe("false")
    expect(screen.queryByRole("menuitemradio", { name: "amber" })).toBeNull()
    expect(selectedItem.querySelector("button")).toBeNull()
    expect(screen.queryByText("themeToggle.custom")).toBeNull()
  })

  it("stays open after selecting a theme or variation", async () => {
    render(<ThemeToggle />, { wrapper: TooltipProvider })

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "themeToggle.changeTheme" }),
      { button: 0, ctrlKey: false }
    )

    const otherItem = themeItem("Other theme")
    fireEvent.click(otherItem)

    await waitFor(() => {
      expect(screen.queryByText("Other theme")).not.toBeNull()
    })

    fireEvent.click(screen.getByRole("menuitemradio", { name: "green" }))

    await waitFor(() => {
      expect(screen.queryByText("Other theme")).not.toBeNull()
    })
  })

  it("repaints its pictures when the system switches between light and dark", async () => {
    render(<ThemeToggle />, { wrapper: TooltipProvider })
    openMenu()
    await screen.findByText("Selected theme")
    expect(variationColor("blue")).toBe("navy")

    appearance.dark = false
    act(() => {
      window.dispatchEvent(new Event("themechange"))
    })

    expect(variationColor("blue")).toBe("blue")
  })

  it("picks the mode from one row and links to Choose menu themes", async () => {
    const { setThemeMode } = await import("@/utils/theme")
    render(<ThemeToggle />, { wrapper: TooltipProvider })
    openMenu()

    const dark = await screen.findByRole("menuitemradio", { name: "themeToggle.dark" })
    expect(dark.getAttribute("aria-checked")).toBe("true")
    fireEvent.click(screen.getByRole("menuitemradio", { name: "themeToggle.light" }))
    await waitFor(() => expect(setThemeMode).toHaveBeenCalledWith("light"))

    const link = screen.getByRole("menuitem", { name: "settings:themes.menu.button" })
    expect(link.getAttribute("href")).toBe("/settings")
  })

  it("shows a hidden theme that the user showed and hides a free theme that the user hid", async () => {
    saveVisibility({ gruvbox: true, other: false })
    render(<ThemeToggle />, { wrapper: TooltipProvider })
    openMenu()

    await screen.findByText("Community theme")
    expect(screen.queryByText("Other theme")).toBeNull()
  })

  it("keeps the theme in use in the menu when it is hidden", async () => {
    saveVisibility({ selected: false })
    render(<ThemeToggle />, { wrapper: TooltipProvider })
    openMenu()

    await screen.findByText("Selected theme")
  })

  it("follows a choice that arrives from the server", async () => {
    render(<ThemeToggle />, { wrapper: TooltipProvider })
    openMenu()
    await screen.findByText("Other theme")

    act(() => {
      applyServerSettings({ "qui-theme-visibility": JSON.stringify({ other: false }) })
    })

    await waitFor(() => expect(screen.queryByText("Other theme")).toBeNull())
  })
})
