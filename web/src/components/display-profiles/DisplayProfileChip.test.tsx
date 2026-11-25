/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  _resetClientSettingsForTests,
  applyServerSettings,
  createDisplayProfile,
  markClientSettingsUnavailable,
  parseJsonBoolean,
  pickDisplayProfile,
  seedAndMarkReady,
  useClientSetting,
  writeClientSetting
} from "@/lib/client-settings"
import { _resetFeatureNoticesForTests } from "@/components/feature-notice/due-notice"
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

async function pressAndExpectFocus(key: string, expected: () => HTMLElement): Promise<void> {
  fireEvent.keyDown(document.activeElement as Element, { key })
  await waitFor(() => expect(document.activeElement).toBe(expected()))
}

async function openPickerOnLaptop(): Promise<void> {
  openMenu()
  await waitFor(() => expect(document.activeElement).toBe(row("Laptop")))
}

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

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("DisplayProfileChip", () => {
  beforeEach(markClientSettingsUnavailable)

  it("lets the user pick or make a profile only after the first read of the server ends", () => {
    createDisplayProfile("Laptop")
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    _resetClientSettingsForTests()
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
    openMenu()
    const newProfile = () => screen.getByRole("menuitem", { name: "displayProfiles.newProfile" })

    act(() => fireEvent.click(row("Laptop")))
    expect(row("displayProfiles.defaultName").getAttribute("aria-checked")).toBe("true")
    expect(row("Laptop").getAttribute("aria-disabled")).toBe("true")
    expect(newProfile().getAttribute("aria-disabled")).toBe("true")

    act(() => markClientSettingsUnavailable())

    expect(newProfile().getAttribute("aria-disabled")).toBeNull()
    act(() => fireEvent.click(row("Laptop")))
    expect(chip().textContent).toBe("Laptop")
  })

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
    await pressAndExpectFocus("ArrowUp", () => row("displayProfiles.defaultName"))
    await pressAndExpectFocus("End", () => screen.getByRole("menuitem", { name: "displayProfiles.manage" }))
    await pressAndExpectFocus("Home", () => row("displayProfiles.defaultName"))
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
    expect(screen.getByRole("menuitemradio", { name: "displayProfiles.defaultName" }).getAttribute("aria-checked")).toBe("true")
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

  it.each([
    ["Esc", () => fireEvent.keyDown(document.activeElement as Element, { key: "Escape" })],
    ["Create", () => {
      fireEvent.change(screen.getByLabelText("displayProfiles.newDialog.name"), { target: { value: "Laptop" } })
      fireEvent.click(screen.getByRole("button", { name: "displayProfiles.newDialog.create" }))
    }],
  ])("%s in the New profile dialog moves focus back to the chip", async (_, close) => {
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
    openMenu()
    fireEvent.click(screen.getByRole("menuitem", { name: "displayProfiles.newProfile" }))
    await act(() => new Promise((resolve) => requestAnimationFrame(resolve)))

    close()

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(chip()))
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

describe("DisplayProfileChip Feature notice", () => {
  const noticeTitle = "torrents:featureNotice.displayProfiles.title"
  const noticeShown = () => screen.queryByText(noticeTitle) !== null

  function loadServerSnapshot(): void {
    act(() => {
      applyServerSettings({})
      seedAndMarkReady({})
    })
  }

  function renderChip(): void {
    render(<TooltipProvider><DisplayProfileChip /></TooltipProvider>)
  }

  function reloadPage(): void {
    cleanup()
    _resetFeatureNoticesForTests()
    renderChip()
  }

  beforeEach(() => {
    _resetFeatureNoticesForTests()
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }))
    writeClientSetting("qui-feature-notice-seen:group-copies", true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("opening the picker hides the notice for good", async () => {
    loadServerSnapshot()
    renderChip()
    expect(noticeShown()).toBe(true)

    openMenu()
    expect(noticeShown()).toBe(false)
    await pressAndExpectFocus("Escape", chip)
    expect(noticeShown()).toBe(false)

    reloadPage()
    expect(noticeShown()).toBe(false)
  })

  it("shows nothing when a second profile exists", () => {
    createDisplayProfile("Laptop")
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    loadServerSnapshot()
    renderChip()

    expect(noticeShown()).toBe(false)
  })

  it("the button opens the New profile dialog", () => {
    loadServerSnapshot()
    renderChip()

    fireEvent.click(screen.getByRole("button", { name: "torrents:featureNotice.displayProfiles.action" }))

    expect(noticeShown()).toBe(false)
    expect(screen.getByRole("dialog")).not.toBeNull()
    expect(screen.getByLabelText("displayProfiles.newDialog.name")).not.toBeNull()
  })
})
