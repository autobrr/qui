/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

// The tooltip wrapper reads "ontouchstart" in window to decide whether taps open the tooltip.
const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  touchWindow.ontouchstart = null
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

function renderTooltip(trigger: ReactNode) {
  render(
    <TooltipProvider>
      <Tooltip>
        {trigger}
        <TooltipContent>Tooltip text</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function tap(element: Element) {
  fireEvent.pointerDown(element, { pointerType: "touch" })
  fireEvent.pointerUp(element, { pointerType: "touch" })
  fireEvent.click(element)
}

const tooltipShown = () => screen.queryByRole("tooltip") !== null

// regression: #4263
describe("TooltipTrigger on a touch device", () => {
  it("opens on a tap of text or an icon", () => {
    renderTooltip(<TooltipTrigger asChild><span>12 GiB</span></TooltipTrigger>)

    tap(screen.getByText("12 GiB"))

    expect(tooltipShown()).toBe(true)
  })

  it("runs the action of a button and stays closed", () => {
    const onClick = vi.fn()
    renderTooltip(<TooltipTrigger asChild><button type="button" onClick={onClick}>Pause</button></TooltipTrigger>)

    tap(screen.getByRole("button", { name: "Pause" }))

    expect(onClick).toHaveBeenCalledOnce()
    expect(tooltipShown()).toBe(false)
  })

  it("stays closed when the icon sits inside a link", () => {
    renderTooltip(
      <a href="/instances/1" onClick={(event) => event.preventDefault()}>
        Instance
        <TooltipTrigger asChild><span>RSS</span></TooltipTrigger>
      </a>
    )

    tap(screen.getByText("RSS"))

    expect(tooltipShown()).toBe(false)
  })

  // A disabled Button has pointer-events: none, so the tap lands on the wrapper, not on the button.
  it("opens on a tap of the wrapper around a disabled button, to explain why", () => {
    renderTooltip(<TooltipTrigger asChild><span data-testid="wrapper"><button type="button" disabled>Run</button></span></TooltipTrigger>)

    tap(screen.getByTestId("wrapper"))

    expect(tooltipShown()).toBe(true)
  })

  it("opens on a tap of a disabled option, to explain why", () => {
    renderTooltip(<TooltipTrigger asChild><div><div role="option" aria-selected="false" aria-disabled="true">Keep files</div></div></TooltipTrigger>)

    tap(screen.getByRole("option", { name: "Keep files" }))

    expect(tooltipShown()).toBe(true)
  })

  it("stays closed on a label that toggles a checkbox, and opens when the checkbox is disabled", () => {
    renderTooltip(<TooltipTrigger asChild><label><input type="checkbox" />Include</label></TooltipTrigger>)
    tap(screen.getByText("Include"))
    expect(tooltipShown()).toBe(false)
    cleanup()

    renderTooltip(<TooltipTrigger asChild><label><input type="checkbox" disabled />Include</label></TooltipTrigger>)
    tap(screen.getByText("Include"))
    expect(tooltipShown()).toBe(true)
  })

  it("opens on a button marked openOnTap", () => {
    renderTooltip(<TooltipTrigger asChild openOnTap><button type="button" aria-label="About grouping">i</button></TooltipTrigger>)

    tap(screen.getByRole("button", { name: "About grouping" }))

    expect(tooltipShown()).toBe(true)
  })

  it("opens on a bare trigger, which renders its own button", () => {
    renderTooltip(<TooltipTrigger aria-label="More information">i</TooltipTrigger>)

    tap(screen.getByRole("button", { name: "More information" }))

    expect(tooltipShown()).toBe(true)
  })
})
