/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
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
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, search, children, ...props }: { to: string; search?: Record<string, string>; children: ReactNode }) => (
    <a href={`${to}?${new URLSearchParams(search)}`} {...props}>{children}</a>
  ),
}))

import { DisplayProfileChip } from "./DisplayProfileChip"

function SidebarProbe() {
  const [collapsed] = useClientSetting("qui-filter-sidebar-collapsed", { defaultValue: false, parse: parseJsonBoolean })
  return <output>{collapsed ? "collapsed" : "open"}</output>
}

function chip(): HTMLElement {
  return screen.getByRole("button", { name: "displayProfiles.chipAriaLabel" })
}

function openMenu(): void {
  fireEvent.pointerDown(chip(), { button: 0, ctrlKey: false })
}

function row(name: string): HTMLElement {
  return screen.getByRole("menuitemradio", { name })
}

/** Radix moves the focus of a menu in a timeout, so the check waits for it. */
async function pressAndExpectFocus(key: string, expected: () => HTMLElement): Promise<void> {
  fireEvent.keyDown(document.activeElement as Element, { key })
  await waitFor(() => expect(document.activeElement).toBe(expected()))
}

/** Opens the picker of renderWithThreeProfiles, and waits for the focus on the row of Laptop. */
async function openPickerOnLaptop(): Promise<void> {
  openMenu()
  await waitFor(() => expect(document.activeElement).toBe(row("Laptop")))
}

/** Default, Laptop, and Phone, with this browser on Laptop: the row in the middle. */
function renderWithThreeProfiles(): void {
  createDisplayProfile("Phone")
  const laptop = createDisplayProfile("Laptop")
  pickDisplayProfile(laptop)
  render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
})

afterEach(cleanup)

describe("DisplayProfileChip", () => {
  it("opens with focus on the row of this browser's profile", async () => {
    renderWithThreeProfiles()
    openMenu()

    await waitFor(() => expect(document.activeElement).toBe(row("Laptop")))
  })

  it("Up, Down, Home, and End move between the rows", async () => {
    renderWithThreeProfiles()
    await openPickerOnLaptop()

    await pressAndExpectFocus("ArrowDown", () => row("Phone"))
    await pressAndExpectFocus("ArrowUp", () => row("Laptop"))
    await pressAndExpectFocus("ArrowUp", () => row("Default"))
    await pressAndExpectFocus("End", () => screen.getByRole("menuitem", { name: "displayProfiles.manage" }))
    await pressAndExpectFocus("Home", () => row("Default"))
  })

  it("the first letter of a name moves to that row", async () => {
    renderWithThreeProfiles()
    await openPickerOnLaptop()

    await pressAndExpectFocus("p", () => row("Phone"))
  })

  it("Enter picks the row with focus, and the chip then shows it", async () => {
    renderWithThreeProfiles()
    await openPickerOnLaptop()
    await pressAndExpectFocus("ArrowDown", () => row("Phone"))

    fireEvent.keyDown(row("Phone"), { key: "Enter" })

    expect(screen.queryByRole("menu")).toBeNull()
    expect(chip().textContent).toBe("Phone")
  })

  it("Esc closes the picker, keeps the pick, and moves focus back to the chip", async () => {
    renderWithThreeProfiles()
    await openPickerOnLaptop()
    await pressAndExpectFocus("ArrowDown", () => row("Phone"))

    await pressAndExpectFocus("Escape", chip)

    expect(screen.queryByRole("menu")).toBeNull()
    expect(chip().textContent).toBe("Laptop")
  })

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

  it("Manage profiles… links to the Display profiles section of Settings", () => {
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
    openMenu()

    const manage = screen.getByRole("menuitem", { name: "displayProfiles.manage" })

    expect(manage.tagName).toBe("A")
    expect(manage.getAttribute("href")).toBe("/settings?tab=display-profiles")
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
