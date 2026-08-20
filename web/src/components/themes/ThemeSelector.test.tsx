/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const fixture = vi.hoisted(() => {
  const makeTheme = (
    id: string,
    name: string,
    options: Pick<Theme, "isPremium" | "isCustom"> = {}
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
      makeTheme("premium", "Premium theme", { isPremium: true }),
      makeTheme("free", "Free theme"),
    ],
    customThemes: [
      makeTheme("custom:local", "Custom theme", { isCustom: true }),
    ],
  }
})

vi.mock("@/config/themes", () => ({
  themes: fixture.builtInThemes,
  isThemePremium: (id: string) =>
    fixture.builtInThemes.find((theme) => theme.id === id)?.isPremium ?? false,
}))

vi.mock("@/hooks/useLicense.ts", () => ({
  useHasPremiumAccess: () => ({
    hasPremiumAccess: true,
    isLoading: false,
    isError: false,
  }),
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

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}))

import { ThemeSelector } from "@/components/themes/ThemeSelector"

afterEach(cleanup)

describe("ThemeSelector", () => {
  it("presents built-in and custom themes as one ordered catalog", () => {
    render(<ThemeSelector />)

    const catalog = screen.getByRole("list", { name: "themes.selector.title" })
    const cards = within(catalog).getAllByRole("listitem")

    expect(cards).toHaveLength(3)
    expect(cards.map((card) => card.textContent)).toEqual([
      expect.stringContaining("Free theme"),
      expect.stringContaining("Premium theme"),
      expect.stringContaining("Custom theme"),
    ])
    expect(screen.getByText("themes.custom.title")).toBeTruthy()
  })
})
