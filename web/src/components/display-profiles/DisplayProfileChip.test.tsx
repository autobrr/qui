/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  createDisplayProfile,
  parseJsonBoolean,
  pickDisplayProfile,
  useClientSetting,
  writeClientSetting
} from "@/lib/client-settings"
import { TooltipProvider } from "@/components/ui/tooltip"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
}))
const instancesMock = vi.hoisted(() => ({ instances: [] }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instancesMock }))

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
    render(<TooltipProvider><DisplayProfileChip /><SidebarProbe /></TooltipProvider>)
    expect(screen.getByRole("status").textContent).toBe("open")

    openMenu()
    const laptop = screen.getByRole("menuitemradio", { name: "Laptop" })
    expect(screen.getByRole("menuitemradio", { name: "Default" }).getAttribute("aria-checked")).toBe("true")
    act(() => fireEvent.click(laptop))

    expect(screen.getByRole("status").textContent).toBe("collapsed")
  })

  it("the New profile item opens the dialog with focus in the name field", async () => {
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
    openMenu()
    fireEvent.click(screen.getByRole("menuitem", { name: "displayProfiles.newProfile" }))
    await act(() => new Promise((resolve) => requestAnimationFrame(resolve)))

    expect(document.activeElement).toBe(screen.getByLabelText("displayProfiles.newDialog.name"))
  })

  it("creates a profile from the dialog, and the chip then shows it", () => {
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
    openMenu()
    fireEvent.click(screen.getByRole("menuitem", { name: "displayProfiles.newProfile" }))

    fireEvent.change(screen.getByLabelText("displayProfiles.newDialog.name"), { target: { value: "Laptop" } })
    fireEvent.click(screen.getByRole("button", { name: "displayProfiles.newDialog.create" }))

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(screen.getByRole("button", { name: "displayProfiles.chipAriaLabel" }).textContent).toBe("Laptop")
    openMenu()
    expect(screen.getByRole("menuitemradio", { name: "Laptop" }).getAttribute("aria-checked")).toBe("true")
  })
})
