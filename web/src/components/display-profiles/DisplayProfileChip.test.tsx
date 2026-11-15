/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  createDisplayProfile,
  parseJsonBoolean,
  pickDisplayProfile,
  useClientSetting,
  writeClientSetting
} from "@/lib/client-settings"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { DisplayProfileChip } from "./DisplayProfileChip"

function SidebarProbe() {
  const [collapsed] = useClientSetting("qui-filter-sidebar-collapsed", { defaultValue: false, parse: parseJsonBoolean })
  return <output>{collapsed ? "collapsed" : "open"}</output>
}

function openMenu(): void {
  fireEvent.pointerDown(screen.getByRole("button", { name: "displayProfiles.chipAriaLabel" }), { button: 0, ctrlKey: false })
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
})

afterEach(cleanup)

describe("DisplayProfileChip", () => {
  it("a pick in the list changes the value that a hook reads", () => {
    createDisplayProfile("Laptop")
    writeClientSetting("qui-filter-sidebar-collapsed", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    render(<><DisplayProfileChip /><SidebarProbe /></>)
    expect(screen.getByRole("status").textContent).toBe("open")

    openMenu()
    const laptop = screen.getByRole("menuitemradio", { name: "Laptop" })
    expect(screen.getByRole("menuitemradio", { name: "Default" }).getAttribute("aria-checked")).toBe("true")
    act(() => fireEvent.click(laptop))

    expect(screen.getByRole("status").textContent).toBe("collapsed")
  })

  it("creates a profile by name from the panel, and refuses a name in use", () => {
    render(<DisplayProfileChip />)
    openMenu()
    fireEvent.click(screen.getByRole("menuitem", { name: "displayProfiles.manage" }))

    const nameField = screen.getByLabelText("displayProfiles.panel.newName")
    fireEvent.change(nameField, { target: { value: "default" } })
    fireEvent.click(screen.getByRole("button", { name: "displayProfiles.panel.create" }))
    expect(screen.getByText("displayProfiles.nameErrors.duplicate")).not.toBeNull()

    fireEvent.change(nameField, { target: { value: "Laptop" } })
    fireEvent.click(screen.getByRole("button", { name: "displayProfiles.panel.create" }))

    const laptopRow = within(screen.getByRole("dialog")).getByText("Laptop").closest("li")
    expect(laptopRow?.textContent).toContain("displayProfiles.panel.thisBrowser")
    expect(screen.getByRole("button", { name: "displayProfiles.panel.use" })).not.toBeNull()
  })
})
