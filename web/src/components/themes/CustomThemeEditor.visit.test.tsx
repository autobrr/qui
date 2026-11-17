/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// The visit starts from the address at page load, so it is set before the modules load.
vi.hoisted(() => window.history.replaceState(null, "", "/?theme=default"))

import { TooltipProvider } from "@/components/ui/tooltip"
import { registerBuiltinThemes } from "@/config/themes"
import { setTheme } from "@/utils/theme"

vi.mock("@/utils/fontLoader", () => ({ loadThemeFonts: vi.fn() }))
vi.mock("@/hooks/useDebounce", () => ({ useDebounce: (value: unknown) => value }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

import { CustomThemeEditor } from "@/components/themes/CustomThemeEditor"

const SAVED = "/* @name: Broken */\n:root { --background: black; --foreground: black; }\n.dark { --background: black; --foreground: black; }\n"

function customStyle(): string | null {
  return document.getElementById("custom-theme-style")?.textContent ?? null
}

beforeEach(async () => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })))
  registerBuiltinThemes([{
    id: "minimal",
    name: "Minimal",
    cssVars: { light: { "--background": "white" }, dark: { "--background": "black" } },
  }])
  await setTheme("minimal", undefined, undefined, true)
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe("CustomThemeEditor during a default theme visit", () => {
  // regression: #4124
  it("previews an edited theme only after a change, because the saved CSS can hide the page", async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <CustomThemeEditor target={{ kind: "edit", id: "broken", filename: "broken.css", css: SAVED }} onClose={() => {}} />
        </TooltipProvider>
      </QueryClientProvider>
    )
    const css = screen.getByLabelText("themes.custom.editor.css")
    expect(customStyle()).toBeNull()

    const repaired = SAVED.replace("--foreground: black; }\n.dark", "--foreground: white; }\n.dark")
    fireEvent.change(css, { target: { value: repaired } })
    expect(customStyle()).toBe(repaired)

    fireEvent.change(css, { target: { value: SAVED } })
    await act(async () => {})
    expect(customStyle()).toBeNull()
    expect(document.documentElement.getAttribute("data-theme")).toBe("minimal")
  })
})
