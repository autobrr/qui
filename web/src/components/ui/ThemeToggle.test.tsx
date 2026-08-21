/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

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
        "--variation-blue": "blue",
        "--variation-green": "green",
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
  const premiumTheme = makeTheme("premium", "Premium theme", {
    isPremium: true,
    locked: true,
  })
  const customTheme = makeTheme("custom:local", "Custom theme", {
    isCustom: true,
  })

  return {
    builtInThemes: [selectedTheme, premiumTheme, otherTheme],
    customThemes: [customTheme],
    selectedTheme,
  }
})

vi.mock("@/config/themes", () => ({
  themes: fixture.builtInThemes,
  getThemeById: (id: string) =>
    [...fixture.builtInThemes, ...fixture.customThemes].find((theme) => theme.id === id),
  isThemePremium: (id: string) =>
    fixture.builtInThemes.find((theme) => theme.id === id)?.isPremium ?? false,
}))

vi.mock("@/utils/theme", () => ({
  getCurrentThemeMode: () => "dark",
  getCurrentTheme: () => fixture.selectedTheme,
  getThemeVariation: (id?: string) => id === "other" ? "amber" : "blue",
  setTheme: vi.fn(),
  setThemeMode: vi.fn(),
  setThemeVariation: vi.fn(),
}))

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

afterEach(cleanup)

function themeItem(name: string): HTMLElement {
  const item = screen.getByText(name).closest("[role=menuitem]")
  if (!(item instanceof HTMLElement)) {
    throw new Error(`Theme menu item not found: ${name}`)
  }
  return item
}

function swatchCount(item: HTMLElement): number {
  return item.querySelectorAll("[style]").length
}

describe("ThemeToggle", () => {
  it("shows the complete catalog and every variation without repeated badges", async () => {
    render(<ThemeToggle />)

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "themeToggle.changeTheme" }),
      { button: 0, ctrlKey: false }
    )

    const selected = await screen.findByText("Selected theme")
    const selectedItem = selected.closest("[role=menuitem]") as HTMLElement
    const otherName = screen.getByText("Other theme")
    const otherItem = themeItem("Other theme")
    const premiumItem = themeItem("Premium theme")
    const customItem = themeItem("Custom theme")
    const items = screen.getAllByRole("menuitem")

    expect(items.indexOf(selectedItem)).toBeLessThan(items.indexOf(otherItem))
    expect(items.indexOf(otherItem)).toBeLessThan(items.indexOf(premiumItem))
    expect(items.indexOf(premiumItem)).toBeLessThan(items.indexOf(customItem))
    expect(premiumItem.hasAttribute("data-disabled")).toBe(true)

    await waitFor(() => expect(swatchCount(selectedItem)).toBe(3))
    expect(swatchCount(otherItem)).toBe(2)
    expect(otherName.classList.contains("truncate")).toBe(false)

    const variationRow = otherItem.querySelector("[data-slot=\"theme-variations\"]")
    expect(variationRow).not.toBeNull()
    expect(variationRow?.querySelectorAll("[style]")).toHaveLength(1)
    expect(screen.queryByText("themeToggle.premium")).toBeNull()
    expect(screen.queryByText("themeToggle.custom")).toBeNull()
  })

  it("stays open after selecting a theme or variation", async () => {
    render(<ThemeToggle />)

    fireEvent.pointerDown(
      screen.getByRole("button", { name: "themeToggle.changeTheme" }),
      { button: 0, ctrlKey: false }
    )

    const otherItem = themeItem("Other theme")
    fireEvent.click(otherItem)

    await waitFor(() => {
      expect(screen.queryByText("Other theme")).not.toBeNull()
    })

    fireEvent.click(screen.getByTitle("amber"))

    await waitFor(() => {
      expect(screen.queryByText("Other theme")).not.toBeNull()
    })
  })
})
