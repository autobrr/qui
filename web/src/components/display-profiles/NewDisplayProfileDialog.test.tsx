/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { _resetClientSettingsForTests, markClientSettingsUnavailable, getDisplayProfiles } from "@/lib/client-settings"

import { NewDisplayProfileDialog } from "./NewDisplayProfileDialog"

function renderDialog() {
  const onOpenChange = vi.fn()
  render(<NewDisplayProfileDialog open onOpenChange={onOpenChange} />)
  return { onOpenChange }
}

function typeName(value: string): void {
  fireEvent.change(screen.getByLabelText("Name"), { target: { value } })
}

function iconButton(): HTMLElement {
  return screen.getByRole("button", { name: /^Icon:/ })
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  markClientSettingsUnavailable()
})

afterEach(cleanup)

describe("NewDisplayProfileDialog", () => {
  it("names the profile that it copies", () => {
    renderDialog()

    expect(screen.getByText(/columns, density, sidebars, and filters/).textContent)
      .toBe("Copies Default: columns, density, sidebars, and filters. This browser then uses it.")
  })

  it("counts the characters of the name out of 32", () => {
    renderDialog()

    typeName("  Laptop ")

    expect(screen.getByText("6/32")).not.toBeNull()
  })

  it("shows the name errors inline and creates nothing", () => {
    const { onOpenChange } = renderDialog()

    typeName("default")
    expect(screen.getByText("Another profile has this name.")).not.toBeNull()

    typeName("x".repeat(33))
    expect(screen.getByText("Use 32 characters or fewer.")).not.toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Create" }))

    expect(getDisplayProfiles().profiles).toHaveLength(1)
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  it("creates nothing before the first read of the server ends", () => {
    _resetClientSettingsForTests()
    renderDialog()
    typeName("Laptop")

    fireEvent.submit(screen.getByRole("button", { name: "Create" }).closest("form")!)
    expect(getDisplayProfiles().profiles).toHaveLength(1)

    act(() => markClientSettingsUnavailable())
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    expect(getDisplayProfiles().profiles).toHaveLength(2)
  })

  it("guesses the icon from the name while the user types", () => {
    renderDialog()
    expect(iconButton().getAttribute("aria-label")).toContain("Bookmark")

    typeName("My phone")
    expect(iconButton().getAttribute("aria-label")).toContain("Smartphone")
    typeName("Work laptop")
    expect(iconButton().getAttribute("aria-label")).toContain("Laptop")
    typeName("Living room TV")
    expect(iconButton().getAttribute("aria-label")).toContain("TV")
  })

  it("creates the profile with the guessed icon and no color, and this browser uses it", () => {
    const { onOpenChange } = renderDialog()

    typeName("iPad")
    fireEvent.click(screen.getByRole("button", { name: "Create" }))

    const { active, profiles } = getDisplayProfiles()
    expect(profiles).toHaveLength(2)
    expect(active).toMatchObject({ name: "iPad", icon: "tablet", color: null, isDefault: false })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("stops the guess when the user picks an icon", () => {
    renderDialog()
    typeName("Desk")

    fireEvent.click(iconButton())
    fireEvent.click(screen.getByRole("button", { name: "Rocket" }))
    typeName("Desktop phone")
    expect(iconButton().getAttribute("aria-label")).toContain("Rocket")
    fireEvent.click(screen.getByRole("button", { name: "Create" }))

    expect(getDisplayProfiles().active.icon).toBe("rocket")
  })
})
