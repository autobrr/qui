/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  touchWindow.ontouchstart = null
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
  fireEvent(element, new PointerEvent("click", { bubbles: true, cancelable: true, pointerType: "mouse" }))
}

const tooltipShown = () => screen.queryByRole("tooltip") !== null

describe("Tooltip with disabled", () => {
  it("stays closed on a click, and is still closed when disabled goes away", () => {
    const tree = (disabled: boolean) => (
      <TooltipProvider>
        <Tooltip disabled={disabled}>
          <TooltipTrigger asChild><a href="#stats">Statistics</a></TooltipTrigger>
          <TooltipContent>Tooltip text</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
    const { rerender } = render(tree(true))

    fireEvent.click(screen.getByText("Statistics"))
    expect(tooltipShown()).toBe(false)

    rerender(tree(false))
    expect(tooltipShown()).toBe(false)

    fireEvent.click(screen.getByText("Statistics"))
    expect(tooltipShown()).toBe(true)
  })

  it("closes an open tooltip, and does not open it again when disabled goes away", () => {
    const tree = (disabled: boolean) => (
      <TooltipProvider>
        <Tooltip disabled={disabled}>
          <TooltipTrigger asChild><span>12 GiB</span></TooltipTrigger>
          <TooltipContent>Tooltip text</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
    const { rerender } = render(tree(false))
    tap(screen.getByText("12 GiB"))
    expect(tooltipShown()).toBe(true)

    rerender(tree(true))
    expect(tooltipShown()).toBe(false)

    rerender(tree(false))
    expect(tooltipShown()).toBe(false)
  })

  it("keeps the touch guard after disabled closes a tapped tooltip", () => {
    const tree = (disabled: boolean) => (
      <TooltipProvider>
        <Tooltip disabled={disabled}>
          <TooltipTrigger asChild><span tabIndex={0}>12 GiB</span></TooltipTrigger>
          <TooltipContent>Tooltip text</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
    const { rerender } = render(tree(false))
    tap(screen.getByText("12 GiB"))
    rerender(tree(true))
    rerender(tree(false))

    fireEvent.focus(screen.getByText("12 GiB"))
    expect(tooltipShown()).toBe(false)
  })
})

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

  it("still opens on a mouse click of a button, as on a trackpad", () => {
    renderTooltip(<TooltipTrigger asChild><button type="button">Pause</button></TooltipTrigger>)

    const button = screen.getByRole("button", { name: "Pause" })
    fireEvent.pointerDown(button, { pointerType: "mouse" })
    fireEvent.click(button)

    expect(tooltipShown()).toBe(true)
  })

  it("opens on a button marked openOnTap", () => {
    renderTooltip(<TooltipTrigger asChild openOnTap><button type="button" aria-label="About grouping">i</button></TooltipTrigger>)

    tap(screen.getByRole("button", { name: "About grouping" }))

    expect(tooltipShown()).toBe(true)
  })

  it("opens on a pen tap of a trigger marked openOnTap only, and keeps the tap from the row", () => {
    const openRow = vi.fn()
    renderTooltip(
      <div role="button" tabIndex={0} onClick={openRow}>
        <TooltipTrigger asChild openOnTap="only"><span>Files over SSH</span></TooltipTrigger>
      </div>
    )

    const fact = screen.getByText("Files over SSH")
    fireEvent.pointerDown(fact, { pointerType: "pen" })
    fireEvent(fact, new PointerEvent("click", { bubbles: true, pointerType: "mouse" }))

    expect(tooltipShown()).toBe(true)
    expect(openRow).not.toHaveBeenCalled()
  })

  it("opens on a bare trigger, which renders its own button", () => {
    renderTooltip(<TooltipTrigger aria-label="More information">i</TooltipTrigger>)

    tap(screen.getByRole("button", { name: "More information" }))

    expect(tooltipShown()).toBe(true)
  })
})
