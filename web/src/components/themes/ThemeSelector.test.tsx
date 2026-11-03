/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { http, HttpResponse } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { _resetClientSettingsForTests, seedAndMarkReady } from "@/lib/client-settings"
import { server } from "@/test/msw/server"

const fixture = vi.hoisted(() => {
  const makeTheme = (
    id: string,
    name: string,
    options: Pick<Theme, "isPremium" | "isCustom" | "locked"> = {}
  ): Theme => ({
    id,
    name,
    description: `${name} description`,
    ...options,
    cssVars: {
      light: {},
      dark: {},
    },
  })

  return {
    builtInThemes: [
      makeTheme("nord", "Licensed theme", { isPremium: true }),
      makeTheme("locked", "Locked theme", { isPremium: true, locked: true }),
      makeTheme("free", "Free theme"),
      makeTheme("gruvbox", "Community theme", { isPremium: true }),
    ],
    customThemes: [
      makeTheme("custom:local", "Custom theme", { isCustom: true }),
    ],
  }
})

vi.mock("@/config/themes", () => ({
  themes: fixture.builtInThemes,
  getThemeById: (id: string) =>
    fixture.builtInThemes.find((theme) => theme.id === id),
}))

vi.mock("@/hooks/useLicense.ts", () => ({
  useHasPremiumAccess: () => ({
    hasPremiumAccess: true,
    isLoading: false,
    isError: false,
  }),
}))

const builtinsResult = vi.hoisted(() => ({ data: undefined, isSuccess: true, isError: false }))
vi.mock("@/hooks/useBuiltinThemes", () => ({
  useBuiltinThemes: () => builtinsResult,
}))

vi.mock("@/hooks/useCustomThemes", () => ({
  useCustomThemes: () => ({
    customThemes: fixture.customThemes,
    errors: [],
    directory: "/config/themes",
    isFetching: false,
    isError: false,
    refetch: vi.fn(),
  }),
}))

vi.mock("@/hooks/useTheme", () => ({
  useTheme: () => ({
    theme: "free",
    setTheme: vi.fn(),
    setVariation: vi.fn(),
  }),
}))

vi.mock("@/utils/theme", () => ({
  getThemeColors: () => ({
    primary: "blue",
    secondary: "gray",
    accent: "green",
  }),
  getThemeVariation: () => null,
}))

vi.mock("@/components/themes/ThemeLayoutSettings", () => ({
  ThemeLayoutSettings: () => null,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}))

import { ThemeSelector } from "@/components/themes/ThemeSelector"

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
})

afterEach(cleanup)

function themeCards(): HTMLElement[] {
  return within(screen.getByRole("list", { name: "themes.selector.title" })).getAllByRole("listitem")
}

function card(name: string): HTMLElement {
  const match = themeCards().find((item) => item.textContent?.includes(name))
  if (!match) throw new Error(`Theme card not found: ${name}`)
  return match
}

// Opens the client settings push and resolves with the body of the next PUT.
function nextSettingsPush(): Promise<Record<string, string>> {
  seedAndMarkReady({})
  return new Promise((resolve) => {
    server.use(http.put("*/api/client-settings", async ({ request }) => {
      resolve(await request.json() as Record<string, string>)
      return HttpResponse.json({})
    }))
  })
}

describe("ThemeSelector", () => {
  it("presents built-in and custom themes as one ordered catalog", () => {
    render(<ThemeSelector />)

    const catalog = screen.getByRole("list", { name: "themes.selector.title" })
    const cards = within(catalog).getAllByRole("listitem")

    expect(cards).toHaveLength(5)
    expect(cards.map((card) => card.textContent)).toEqual([
      expect.stringContaining("Free theme"),
      expect.stringContaining("Licensed theme"),
      expect.stringContaining("Locked theme"),
      expect.stringContaining("Community theme"),
      expect.stringContaining("Custom theme"),
    ])
    expect(screen.getByText("themes.custom.title")).toBeTruthy()
  })

  it("offers Hide on a shown theme and Show on a hidden theme", () => {
    render(<ThemeSelector />)

    for (const name of ["Free theme", "Licensed theme", "Custom theme"]) {
      expect(within(card(name)).getByRole("button", { name: "themes.selector.hideTheme" })).toBeTruthy()
    }
    expect(within(card("Community theme")).getByRole("button", { name: "themes.selector.showTheme" })).toBeTruthy()
  })

  it("pushes a Hide choice through the client settings", async () => {
    const push = nextSettingsPush()
    render(<ThemeSelector />)

    fireEvent.click(within(card("Free theme")).getByRole("button", { name: "themes.selector.hideTheme" }))
    window.dispatchEvent(new Event("pagehide"))

    expect(await push).toEqual({ "qui-theme-visibility": JSON.stringify({ free: false }) })
    expect(within(card("Free theme")).getByRole("button", { name: "themes.selector.showTheme" })).toBeTruthy()
  })

  it("stores only the changes from the defaults", async () => {
    localStorage.setItem("qui-theme-visibility", JSON.stringify({ gruvbox: true, free: false }))
    const push = nextSettingsPush()
    render(<ThemeSelector />)

    fireEvent.click(within(card("Free theme")).getByRole("button", { name: "themes.selector.showTheme" }))
    window.dispatchEvent(new Event("pagehide"))

    expect(await push).toEqual({ "qui-theme-visibility": JSON.stringify({ gruvbox: true }) })
  })

  it("marks a locked theme with a lock, not a premium badge", () => {
    render(<ThemeSelector />)

    const catalog = screen.getByRole("list", { name: "themes.selector.title" })
    const [lockedCard] = within(catalog)
      .getAllByRole("listitem")
      .filter((card) => card.textContent?.includes("Locked theme"))

    expect(lockedCard.textContent).toContain("themes.selector.locked")
    for (const card of within(catalog).getAllByRole("listitem")) {
      expect(card.textContent).not.toMatch(/premium/i)
    }
  })
})
