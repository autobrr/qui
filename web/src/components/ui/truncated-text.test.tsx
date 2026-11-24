/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { TooltipProvider } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"

// The tooltip wrapper reads "ontouchstart" in window to decide whether taps open the tooltip.
const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  delete touchWindow.ontouchstart
})

function layout({ scrollWidth = 50, clientWidth = 50, scrollHeight = 40, clientHeight = 40 }) {
  vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(scrollWidth)
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(clientWidth)
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(scrollHeight)
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(clientHeight)
}

function focusName(text: string) {
  fireEvent.focus(screen.getByText(text))
  return screen.queryByRole("tooltip")?.textContent
}

it("shows the full text when it is cut off at the end of the line", () => {
  layout({ scrollWidth: 200 })
  render(<TooltipProvider><TruncatedText>A very long category name</TruncatedText></TooltipProvider>)

  expect(focusName("A very long category name")).toContain("A very long category name")
})

it("shows the full text when it is clipped to a number of lines", () => {
  layout({ scrollHeight: 60 })
  render(<TooltipProvider><TruncatedText wrap className="line-clamp-2">Some<wbr />.Long<wbr />.Name</TruncatedText></TooltipProvider>)

  expect(focusName("Some.Long.Name")).toContain("Some.Long.Name")
})

it("keeps the open delay of the next tooltip after the pointer passes over text that fits", () => {
  vi.useFakeTimers()
  layout({})
  vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.textContent === "A very long category name" ? 200 : 50
  })
  render(
    <TooltipProvider>
      <TruncatedText>Short name</TruncatedText>
      <TruncatedText>A very long category name</TruncatedText>
    </TooltipProvider>
  )
  const short = screen.getByText("Short name")
  const long = screen.getByText("A very long category name")

  fireEvent.pointerMove(short, { pointerType: "mouse" })
  act(() => vi.advanceTimersByTime(500))
  fireEvent.pointerLeave(short, { pointerType: "mouse" })
  fireEvent.pointerMove(long, { pointerType: "mouse" })

  expect(screen.queryByRole("tooltip")).toBeNull()
  act(() => vi.advanceTimersByTime(400))
  expect(screen.queryByRole("tooltip")?.textContent).toContain("A very long category name")
  vi.useRealTimers()
})

it("shows no tooltip when the text fits", () => {
  layout({})
  render(<TooltipProvider><TruncatedText>Short name</TruncatedText></TooltipProvider>)

  expect(focusName("Short name")).toBeUndefined()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  delete touchWindow.ontouchstart
  cleanup()
})

// regression: #4263
it("shows the full cut-off name on a tap inside a filter row, and the tap still toggles the filter", () => {
  touchWindow.ontouchstart = null
  layout({ scrollWidth: 200 })
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

// regression: #4358
it("highlights the menu item under the pointer when its label fits", () => {
  layout({})
  render(
    <TooltipProvider>
      <DropdownMenu open>
        <DropdownMenuTrigger>Profiles</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem><TruncatedText>Default</TruncatedText></DropdownMenuItem>
          <DropdownMenuItem><TruncatedText>Compact</TruncatedText></DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </TooltipProvider>
  )

  fireEvent.pointerMove(screen.getByText("Compact"), { pointerType: "mouse" })

  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Compact" }))
})
