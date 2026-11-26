/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { DisplayProfileColor } from "@/lib/display-profiles"

import { DisplayProfileIconPicker } from "./DisplayProfileIconPicker"
import { DisplayProfileTile } from "./DisplayProfileTile"

function renderPicker(icon = "laptop", color: DisplayProfileColor | null = null) {
  const onIconChange = vi.fn()
  const onColorChange = vi.fn()
  render(
    <TooltipProvider>
      <DisplayProfileIconPicker icon={icon} color={color} onIconChange={onIconChange} onColorChange={onColorChange}>
        <button type="button">open</button>
      </DisplayProfileIconPicker>
    </TooltipProvider>
  )
  fireEvent.click(screen.getByRole("button", { name: "open" }))
  return { onIconChange, onColorChange }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("DisplayProfileIconPicker", () => {
  it("presses the icon of the profile", () => {
    renderPicker("laptop")

    expect(screen.getByRole("button", { name: "Laptop" }).getAttribute("aria-pressed")).toBe("true")
    expect(screen.getByRole("button", { name: "Monitor" }).getAttribute("aria-pressed")).toBe("false")
  })

  it("a pick calls back and keeps the popover open", () => {
    const { onIconChange } = renderPicker("laptop")

    fireEvent.click(screen.getByRole("button", { name: "Moon" }))

    expect(onIconChange).toHaveBeenCalledWith("moon")
    expect(screen.getByRole("group", { name: "Color" })).not.toBeNull()
  })

  it("the arrow keys move in the grid", () => {
    renderPicker("laptop")
    const laptop = screen.getByRole("button", { name: "Laptop" })
    laptop.focus()

    fireEvent.keyDown(laptop, { key: "ArrowRight" })
    expect(document.activeElement?.getAttribute("aria-label")).toBe("Tablet")
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowDown" })
    expect(document.activeElement?.getAttribute("aria-label")).toBe("Sofa")
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowLeft" })
    expect(document.activeElement?.getAttribute("aria-label")).toBe("Building")
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowUp" })
    expect(document.activeElement?.getAttribute("aria-label")).toBe("Laptop")
  })

  it("the color None is pressed for a profile with no color, and picks null", () => {
    const { onColorChange } = renderPicker("laptop", "sky")
    const none = screen.getByRole("button", { name: "None (text color)" })
    expect(none.getAttribute("aria-pressed")).toBe("false")
    expect(screen.getByRole("button", { name: "Sky" }).getAttribute("aria-pressed")).toBe("true")

    fireEvent.click(none)

    expect(onColorChange).toHaveBeenCalledWith(null)
    cleanup()
    renderPicker("laptop", null)
    expect(screen.getByRole("button", { name: "None (text color)" }).getAttribute("aria-pressed")).toBe("true")
  })
})

describe("DisplayProfileTile", () => {
  it("shows the first letter of the name for an icon name that the list does not know", () => {
    const { container } = render(<DisplayProfileTile profile={{ name: " zed", icon: "not-an-icon", color: "teal" }} />)

    const tile = container.querySelector("[data-slot=display-profile-tile]")
    expect(tile?.textContent).toBe("Z")
    expect(tile?.querySelector("svg")).toBeNull()
  })

  it("shows the icon for a known name", () => {
    const { container } = render(<DisplayProfileTile profile={{ name: "Zed", icon: "rocket", color: null }} />)

    const tile = container.querySelector("[data-slot=display-profile-tile]")
    expect(tile?.textContent).toBe("")
    expect(tile?.querySelector("svg")).not.toBeNull()
  })
})
