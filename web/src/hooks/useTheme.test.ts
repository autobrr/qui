/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

const appearance = vi.hoisted(() => ({ dark: true }))

vi.mock("@/utils/theme", () => ({
  getCurrentTheme: () => ({ id: "minimal" }),
  getCurrentThemeMode: () => "auto",
  getThemeVariation: () => null,
  isDarkMode: () => appearance.dark,
  setTheme: vi.fn(),
  setThemeMode: vi.fn(),
  setThemeVariation: vi.fn(),
}))

import { useTheme } from "@/hooks/useTheme"

describe("useTheme", () => {
  it("follows a system switch between light and dark that keeps the mode and the theme", () => {
    const { result } = renderHook(() => useTheme())
    expect(result.current.dark).toBe(true)

    appearance.dark = false
    act(() => {
      window.dispatchEvent(new CustomEvent("themechange", { detail: { theme: { id: "minimal" }, mode: "auto", variant: null } }))
    })

    expect(result.current.dark).toBe(false)
  })
})
