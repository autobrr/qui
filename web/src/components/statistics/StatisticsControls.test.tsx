/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PeriodPicker } from "@/components/statistics/StatisticsControls"

const mocks = vi.hoisted(() => ({
  i18n: { t: (key: string) => key, i18n: { resolvedLanguage: "en", language: "en" } },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))

function renderPicker(screenKind: "phone" | "desktop") {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  // A phone has no hover and a narrow viewport. A desktop has neither.
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: screenKind === "phone" && (query === "(hover: none)" || query === "(max-width: 639px)"),
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
  render(<PeriodPicker value="30d" onPreset={() => {}} onCustom={() => {}} />)
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("PeriodPicker", () => {
  it("opens the custom range on a touch screen without focus on a day field, so that iOS opens no date picker over it", async () => {
    renderPicker("phone")

    fireEvent.click(screen.getByRole("button", { name: "periods.custom" }))

    const firstDay = await screen.findByLabelText("customRange.from")
    await new Promise(resolve => requestAnimationFrame(resolve))
    expect(document.activeElement).not.toBe(firstDay)
  })

  it("puts the focus on the first day of the custom range on a screen with hover", async () => {
    renderPicker("desktop")

    fireEvent.click(screen.getByText("periods.custom"))

    const firstDay = await screen.findByLabelText("customRange.from")
    await vi.waitFor(() => expect(document.activeElement).toBe(firstDay))
  })
})
