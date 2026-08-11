/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// Passthrough translator with the English value for the one key this component
// reads, so the accessible-name assertion checks real copy.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string) => (key === "fieldHelp.trigger" ? "More information" : key),
  }),
}))

import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Tooltip, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

// The tooltip wrapper reads "ontouchstart" in window to decide whether taps open
// the tooltip. jsdom has no touch support, so tests opt in per case.
const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  // The open tooltip measures its arrow through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  delete touchWindow.ontouchstart
  cleanup()
})

describe("FieldHelp", () => {
  it("gives the trigger an accessible name", () => {
    render(
      <TooltipProvider>
        <FieldHelp>Ratio limit help</FieldHelp>
      </TooltipProvider>
    )

    expect(screen.getByRole("button", { name: "More information" })).toBeTruthy()
  })

  it("opens on tap", () => {
    touchWindow.ontouchstart = null
    render(
      <TooltipProvider>
        <FieldHelp>Ratio limit help</FieldHelp>
      </TooltipProvider>
    )

    const trigger = screen.getByRole("button")
    fireEvent.pointerDown(trigger, { pointerType: "touch" })
    fireEvent.pointerUp(trigger, { pointerType: "touch" })
    fireEvent.click(trigger)

    expect(screen.getByText("Ratio limit help")).toBeTruthy()
  })

  it("renders the trigger as a plain button so it never submits the form", () => {
    render(
      <TooltipProvider>
        <FieldHelp>Ratio limit help</FieldHelp>
      </TooltipProvider>
    )

    expect(screen.getByRole("button").getAttribute("type")).toBe("button")
  })

  it("leaves an asChild trigger without a type attribute", () => {
    render(
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span data-testid="custom-trigger">Help</span>
          </TooltipTrigger>
        </Tooltip>
      </TooltipProvider>
    )

    expect(screen.getByTestId("custom-trigger").getAttribute("type")).toBeNull()
  })

  it("does not activate the labelled control when clicked", () => {
    render(
      <TooltipProvider>
        <Label htmlFor="sw" className="flex items-center gap-1">
          Name
          <FieldHelp>Ratio limit help</FieldHelp>
        </Label>
        <input id="sw" type="checkbox" />
      </TooltipProvider>
    )

    fireEvent.click(screen.getByRole("button"))

    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(false)
  })
})
