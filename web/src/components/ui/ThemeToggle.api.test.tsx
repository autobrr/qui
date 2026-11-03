/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { http, HttpResponse } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { _resetClientSettingsForTests } from "@/lib/client-settings"
import { server } from "@/test/msw/server"

// The theme menu against a fake qui API: the real theme registry, built-in
// and custom theme queries, and license check. Only applying a theme is stubbed.

vi.mock("@/utils/theme", () => ({
  getCurrentThemeMode: () => "dark",
  getCurrentTheme: () => ({ id: "minimal" }),
  getThemeVariation: () => null,
  getAppliedCustomThemeCss: () => null,
  setTheme: vi.fn(() => Promise.resolve()),
  setThemeMode: vi.fn(),
  setThemeVariation: vi.fn(),
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { ThemeToggle } from "@/components/ui/ThemeToggle"

const freeNames = ["Autobrr", "Dragon", "Kyle", "Minimal", "Napster", "Nightwalker", "Swizzin", "Wave"]
const formerPremiumIds = [
  "amber-minimal", "amethyst-haze", "bubblegum", "catppuccin", "claude", "cyberpunk", "kitsune",
  "the-matrix", "nord", "perpetuity", "spreadsheet", "spreadsheet-classic", "synthwave",
  "tangerine", "tokyo-night",
]

function css(name: string): string {
  return `/* @name: ${name}\n */\n:root { --primary: red; }\n.dark { --primary: darkred; }\n`
}

function lockedStub(id: string) {
  return { id, name: `Licensed ${id}`, premium: true, preview: { light: {}, dark: {} } }
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  server.use(
    http.get("*/api/themes", () => HttpResponse.json({
      themes: [
        ...freeNames.map((name) => ({ id: name.toLowerCase(), name, premium: false, css: css(name) })),
        ...formerPremiumIds.map(lockedStub),
        // A community theme that is new to the build.
        lockedStub("gruvbox"),
      ],
    })),
    http.get("*/api/license/licensed", () => HttpResponse.json({ hasPremiumAccess: true })),
    http.get("*/api/themes/custom", () => HttpResponse.json({
      directory: "/config/themes",
      themes: [{ id: "local", filename: "local.css", css: css("Local") }],
    }))
  )
})

afterEach(cleanup)

async function openMenuNames(): Promise<string[]> {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <ThemeToggle />
    </QueryClientProvider>
  )
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "themeToggle.changeTheme" }),
    { button: 0, ctrlKey: false }
  )
  // Wait for both theme lists.
  await screen.findByText("Local")
  await screen.findByText("Licensed tokyo-night")
  return screen.getAllByRole("menuitem")
    .map((item) => item.textContent ?? "")
    .filter((text) => !text.startsWith("themeToggle."))
}

const defaultMenu = [
  ...freeNames,
  ...formerPremiumIds.map((id) => `Licensed ${id}`),
  "Local",
]

describe("ThemeToggle with the qui API", () => {
  it("shows the free, former premium and custom themes when no choice is saved", async () => {
    expect(await openMenuNames()).toEqual(defaultMenu)
  })

  it("ignores a hidden id that matches no theme", async () => {
    localStorage.setItem("qui-theme-visibility", JSON.stringify({ "custom:removed": false }))

    expect(await openMenuNames()).toEqual(defaultMenu)
  })
})
