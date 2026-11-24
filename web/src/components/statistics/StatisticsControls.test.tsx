/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PeriodPicker } from "@/components/statistics/StatisticsControls"
import { stubPhone } from "@/test/stubPhone"

const mocks = vi.hoisted(() => ({
  i18n: { t: (key: string) => key, i18n: { resolvedLanguage: "en", language: "en" } },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))

function renderPicker(screenKind: "upright" | "desktop") {
  stubPhone(screenKind)
  render(<PeriodPicker value="30d" onPreset={() => {}} onCustom={() => {}} />)
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  Reflect.deleteProperty(HTMLInputElement.prototype, "showPicker")
})

describe("PeriodPicker", () => {
  // regression: #4179
  it("opens the custom range on a touch screen without focus on a day field, so that iOS opens no date picker over it", async () => {
    renderPicker("upright")

    fireEvent.click(screen.getByRole("button", { name: "periods.custom" }))

    const firstDay = await screen.findByLabelText("customRange.from")
    await new Promise(resolve => requestAnimationFrame(resolve))
    expect(document.activeElement).not.toBe(firstDay)
  })

  // regression: #4179
  it("puts the focus on the first day of the custom range on a screen with hover", async () => {
    renderPicker("desktop")

    fireEvent.click(screen.getByText("periods.custom"))

    const firstDay = await screen.findByLabelText("customRange.from")
    await vi.waitFor(() => expect(document.activeElement).toBe(firstDay))
  })

  it("keeps the date picker of the device for a tap that iOS Safari reports as a mouse click", async () => {
    const showPicker = vi.fn()
    Object.defineProperty(HTMLInputElement.prototype, "showPicker", { value: showPicker, configurable: true })
    renderPicker("upright")
    fireEvent.click(screen.getByRole("button", { name: "periods.custom" }))
    const firstDay = await screen.findByLabelText("customRange.from")

    fireEvent.pointerDown(firstDay, { pointerType: "touch" })
    const tap = new PointerEvent("click", { bubbles: true, cancelable: true, pointerType: "mouse" })
    fireEvent(firstDay, tap)
    expect(showPicker).not.toHaveBeenCalled()
    expect(tap.defaultPrevented).toBe(false)

    fireEvent.pointerDown(firstDay, { pointerType: "mouse" })
    const click = new PointerEvent("click", { bubbles: true, cancelable: true, pointerType: "mouse" })
    fireEvent(firstDay, click)
    expect(showPicker).toHaveBeenCalledOnce()
    expect(click.defaultPrevented).toBe(true)
  })
})
