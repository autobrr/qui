/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { registerBuiltinThemes, registerCustomThemes } from "@/config/themes"
import { setTheme, setThemeMode } from "@/utils/theme"

vi.mock("@/utils/fontLoader", () => ({ loadThemeFonts: vi.fn() }))
vi.mock("@/hooks/useDebounce", () => ({ useDebounce: (value: unknown) => value }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

import { CustomThemeEditor } from "@/components/themes/CustomThemeEditor"

const DRAFT = "/* @name: Draft */\n:root { --background: ivory; }\n.dark { --background: navy; }\n"

function customStyle(): string | null {
  return document.getElementById("custom-theme-style")?.textContent ?? null
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  registerBuiltinThemes([{
    id: "minimal",
    name: "Minimal",
    cssVars: { light: { "--background": "white" }, dark: { "--background": "black" } },
  }])
})

afterEach(() => {
  cleanup()
  registerCustomThemes([])
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe("CustomThemeEditor", () => {
  // regression: #4124
  it("keeps the draft on the page through a mode change, and ends it on close", async () => {
    await setTheme("minimal", "dark")
    const client = new QueryClient()
    const { unmount } = render(
      <QueryClientProvider client={client}>
        <CustomThemeEditor target={{ kind: "new" }} onClose={() => {}} />
      </QueryClientProvider>
    )

    fireEvent.change(screen.getByLabelText("themes.custom.editor.css"), { target: { value: DRAFT } })
    expect(customStyle()).toBe(DRAFT)
    expect(document.documentElement.classList.contains("dark")).toBe(true)

    await act(async () => {
      await setThemeMode("light")
    })

    expect(customStyle()).toBe(DRAFT)
    expect(document.documentElement.classList.contains("dark")).toBe(false)

    unmount()
    await act(async () => {})
    expect(customStyle()).toBeNull()
    expect(document.documentElement.getAttribute("data-theme")).toBe("minimal")
  })
})
