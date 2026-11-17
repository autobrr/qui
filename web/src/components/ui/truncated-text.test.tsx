/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { Checkbox } from "@/components/ui/checkbox"
import { TooltipProvider } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"

// The tooltip wrapper reads "ontouchstart" in window to decide whether taps open the tooltip.
const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  touchWindow.ontouchstart = null
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  // jsdom does no layout, so make every element report text wider than its box.
  vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  delete touchWindow.ontouchstart
  cleanup()
})

it("shows the full cut-off name on a tap inside a filter row, and the tap still toggles the filter", () => {
  const onCheckedChange = vi.fn()
  render(
    <TooltipProvider>
      <label>
        <Checkbox onCheckedChange={onCheckedChange} />
        <TruncatedText>A very long category name</TruncatedText>
      </label>
    </TooltipProvider>
  )

  const name = screen.getByText("A very long category name")
  fireEvent.pointerDown(name, { pointerType: "touch" })
  fireEvent.pointerUp(name, { pointerType: "touch" })
  fireEvent.click(name)

  expect(onCheckedChange).toHaveBeenCalledOnce()
  expect(screen.getByRole("tooltip").textContent).toContain("A very long category name")
})
