/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { changeLanguage } from "@/i18n"
import { TooltipProvider } from "@/components/ui/tooltip"
import {
  _resetClientSettingsForTests,
  createDisplayProfile,
  getDisplayProfiles,
  markClientSettingsUnavailable,
  pickDisplayProfile,
  setDisplayProfileIcon,
  setDisplayProfileOwnSortAndFilters,
  TORRENT_VIEW_MODE_KEYS,
  writeClientSetting
} from "@/lib/client-settings"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"

const instancesMock = vi.hoisted(() => ({ instances: [] }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instancesMock }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, search, hash, children, ...props }: { to: string; search?: Record<string, string>; hash?: string; children: ReactNode }) => (
    <a href={`${to}?${new URLSearchParams(search)}${hash ? `#${hash}` : ""}`} {...props}>{children}</a>
  ),
}))

import { DisplayProfilesSettings } from "./DisplayProfilesSettings"

function renderSettings(): void {
  render(<TooltipProvider><DisplayProfilesSettings /></TooltipProvider>)
}

/** Default, Laptop, and Phone with its own sort and filters, with this browser on Laptop. */
function renderWithThreeProfiles(): void {
  const laptop = createDisplayProfile("Laptop")
  setDisplayProfileOwnSortAndFilters(createDisplayProfile("Phone"), true)
  pickDisplayProfile(laptop)
  renderSettings()
}

function list(): HTMLElement {
  return screen.getByRole("list", { name: "Display profiles" })
}

/** The Close button in the footer of an editor. The X in the corner has the same name. */
function footerClose(dialog: HTMLElement): HTMLElement {
  return within(dialog).getAllByRole("button", { name: "Close" }).find((button) => button.dataset.slot !== "dialog-close")!
}

function row(name: string): HTMLElement {
  return within(list()).getByRole("button", { name: new RegExp(`^${name}`) })
}

/** The editor, a region named after the profile that it shows. */
function editor(): HTMLElement {
  return screen.getByRole("region")
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  // The first read of the server ended, so the section takes changes.
  markClientSettingsUnavailable()
})

/** The controls that change a profile, in the editor and in the header. */
function profileActions(): HTMLButtonElement[] {
  const inEditor = within(editor())
  return [
    screen.getByRole("button", { name: "New profile from this setup…" }),
    inEditor.getByRole("button", { name: /^Icon:/ }),
    inEditor.getByRole("button", { name: /^Rename/ }),
    inEditor.getByRole("button", { name: "Use in this browser" }),
    inEditor.getByRole("button", { name: "Delete…" }),
    inEditor.getByRole("switch", { name: "Same columns on every instance" }),
    inEditor.getByRole("switch", { name: "Own sort and filters" }),
  ] as HTMLButtonElement[]
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("DisplayProfilesSettings", () => {
  it("takes a change only after the first read of the server ends, and shows the profiles before that", () => {
    const laptop = createDisplayProfile("Laptop")
    setDisplayProfileOwnSortAndFilters(createDisplayProfile("Phone"), true)
    pickDisplayProfile(laptop)
    _resetClientSettingsForTests()
    renderSettings()
    fireEvent.click(row("Phone"))

    expect(screen.getByRole("region", { name: "Phone" })).toBe(editor())
    expect(profileActions().map((control) => control.disabled)).toEqual(Array(7).fill(true))

    act(() => markClientSettingsUnavailable())

    expect(profileActions().map((control) => control.disabled)).toEqual(Array(7).fill(false))
  })

  it("lists every profile, marks the one of this browser, and opens the editor on it", () => {
    renderWithThreeProfiles()

    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "LaptopThis browser", "Phone"])
    expect(row("Laptop").getAttribute("aria-current")).toBe("true")
    expect(within(list()).getByRole("button", { name: "Own sort and filters" })).not.toBeNull()
    expect(screen.getByRole("region", { name: "Laptop" })).toBe(editor())

    fireEvent.click(row("Phone"))

    expect(screen.getByRole("region", { name: "Phone" })).toBe(editor())
    expect(row("Phone").getAttribute("aria-current")).toBe("true")
  })

  it("Use in this browser picks the profile in the editor, and This browser then takes its place", () => {
    renderWithThreeProfiles()
    fireEvent.click(row("Phone"))

    fireEvent.click(within(editor()).getByRole("button", { name: "Use in this browser" }))

    expect(getDisplayProfiles().active.name).toBe("Phone")
    expect(within(editor()).queryByRole("button", { name: "Use in this browser" })).toBeNull()
    expect(within(editor()).getByRole("button", { name: "This browser" }).getAttribute("aria-disabled")).toBe("true")
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "Laptop", "PhoneThis browser"])
  })

  it("renames inline: the field shows the count and the errors of the name rule, and Enter saves", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(within(editor()).getByRole("button", { name: "Rename Laptop" }))
    const field = within(editor()).getByRole("textbox", { name: "Rename Laptop" })
    expect(document.activeElement).toBe(field)
    expect(within(editor()).getByText("6/32")).not.toBeNull()

    await user.clear(field)
    await user.type(field, "phone{Enter}")
    expect(within(editor()).getByText("Another profile has this name.")).not.toBeNull()
    expect(field.getAttribute("aria-invalid")).toBe("true")

    await user.clear(field)
    await user.type(field, "x".repeat(33))
    expect(within(editor()).getByText("Use 32 characters or fewer.")).not.toBeNull()
    expect(within(editor()).getByText("33/32")).not.toBeNull()

    await user.clear(field)
    await user.type(field, "Work laptop{Enter}")

    expect(within(editor()).queryByRole("textbox")).toBeNull()
    expect(screen.getByRole("region", { name: "Work laptop" })).toBe(editor())
    expect(getDisplayProfiles().active.name).toBe("Work laptop")
    expect(document.activeElement).toBe(within(editor()).getByRole("button", { name: "Rename Work laptop" }))
  })

  it("Esc cancels a rename and keeps the name", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(within(editor()).getByRole("button", { name: "Rename Laptop" }))
    await user.type(within(editor()).getByRole("textbox", { name: "Rename Laptop" }), " 13{Escape}")

    expect(within(editor()).queryByRole("textbox")).toBeNull()
    expect(getDisplayProfiles().active.name).toBe("Laptop")
    expect(document.activeElement).toBe(within(editor()).getByRole("button", { name: "Rename Laptop" }))
  })

  it("Delete… asks first, and the delete then leaves the editor on the profile of this browser", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()
    await user.click(row("Phone"))

    await user.click(within(editor()).getByRole("button", { name: "Delete…" }))
    const confirm = screen.getByRole("alertdialog")
    expect(within(confirm).getByRole("heading").textContent).toBe("Delete Phone?")
    expect(within(confirm).getByText("Other browsers that use it move to Default.")).not.toBeNull()
    await user.click(within(confirm).getByRole("button", { name: "Delete" }))

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(getDisplayProfiles().profiles.map((profile) => profile.name)).toEqual(["", "Laptop"])
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "LaptopThis browser"])
    expect(screen.getByRole("region", { name: "Laptop" })).toBe(editor())
  })

  it("Cancel in the delete dialog keeps the profile", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(within(editor()).getByRole("button", { name: "Delete…" }))
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Cancel" }))

    expect(getDisplayProfiles().profiles.map((profile) => profile.name)).toEqual(["", "Laptop", "Phone"])
  })

  it("holds both switches of a profile", () => {
    renderWithThreeProfiles()
    fireEvent.click(row("Phone"))

    expect(within(editor()).getByRole("switch", { name: "Same columns on every instance" })).not.toBeNull()
    expect(within(editor()).getByRole("switch", { name: "Own sort and filters" }).getAttribute("aria-checked")).toBe("true")
  })

  it("Default has the Same columns switch, a line that it always shares sort and filters, and no Delete…", () => {
    renderWithThreeProfiles()
    fireEvent.click(row("Default"))

    expect(within(editor()).getByRole("switch", { name: "Same columns on every instance" })).not.toBeNull()
    expect(within(editor()).queryByRole("switch", { name: "Own sort and filters" })).toBeNull()
    expect(within(editor()).getByText("Always shares sort and filters")).not.toBeNull()
    expect(within(editor()).queryByRole("button", { name: "Delete…" })).toBeNull()
  })

  it("while only Default exists, a dashed tile opens the New profile dialog, and the editor then shows the new profile", async () => {
    const user = userEvent.setup()
    renderSettings()
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["DefaultThis browser", "Make a profile for a laptop or phone"])

    await user.click(within(list()).getByRole("button", { name: "Make a profile for a laptop or phone" }))
    await user.type(within(screen.getByRole("dialog", { name: "New profile" })).getByLabelText("Name"), "Laptop{Enter}")

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(screen.getByRole("region", { name: "Laptop" })).toBe(editor())
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "LaptopThis browser"])
  })

  it("the header button opens the New profile dialog", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(screen.getByRole("button", { name: "New profile from this setup…" }))

    expect(screen.getByRole("dialog", { name: "New profile" })).not.toBeNull()
  })

  it("names the letter tile of an icon that this version does not know, not the stored icon name", () => {
    setDisplayProfileIcon(createDisplayProfile("Laptop"), "icon-of-a-newer-version")
    renderSettings()

    expect(within(editor()).getByRole("button", { name: "Icon: First letter of the name. Change the icon and the color" })).not.toBeNull()
  })

  it("sums up the row density and the filter sidebar of the profile in the editor", () => {
    createDisplayProfile("Laptop")
    writeClientSetting(TORRENT_VIEW_MODE_KEYS.desktop, "dense", String)
    writeClientSetting("qui-filter-sidebar-collapsed", true)
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    renderSettings()
    expect(within(editor()).getByText("Table rows · filter sidebar open")).not.toBeNull()

    fireEvent.click(row("Laptop"))

    expect(screen.getByRole("region", { name: "Laptop" })).toBe(editor())
    expect(within(editor()).getByText("Dense rows · filter sidebar closed")).not.toBeNull()
  })
})

describe("DisplayProfilesSettings in another language", () => {
  afterEach(async () => {
    await changeLanguage("en")
  })

  it("names Default in the language of the app until the user renames it", async () => {
    const user = userEvent.setup()
    await changeLanguage("de")
    createDisplayProfile("Laptop")
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    renderSettings()
    expect(within(screen.getByRole("list")).getAllByRole("listitem")[0].textContent).toBe("StandardDieser Browser")

    await user.click(within(editor()).getByRole("button", { name: "Standard umbenennen" }))
    const field = within(editor()).getByRole("textbox", { name: "Standard umbenennen" })
    await user.clear(field)
    await user.type(field, "Werkstatt{Enter}")
    await changeLanguage("en")

    expect(await within(list()).findByText("Werkstatt")).not.toBeNull()
  })

  it("a rename of Default that keeps the shown name saves nothing, so Default keeps following the language", async () => {
    const user = userEvent.setup()
    await changeLanguage("de")
    renderSettings()

    await user.click(within(editor()).getByRole("button", { name: "Standard umbenennen" }))
    await user.type(within(editor()).getByRole("textbox", { name: "Standard umbenennen" }), "{Enter}")
    await changeLanguage("en")

    expect(await within(list()).findByText("Default")).not.toBeNull()
  })
})

describe("DisplayProfilesSettings below 768 px", () => {
  beforeEach(() => {
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query === "(max-width: 767px)", addEventListener: () => {}, removeEventListener: () => {} }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("a tap on a row picks a profile only after the first read of the server ends", () => {
    createDisplayProfile("Laptop")
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    _resetClientSettingsForTests()
    renderSettings()

    fireEvent.click(row("Laptop"))
    expect(getDisplayProfiles().active.id).toBe(DEFAULT_DISPLAY_PROFILE_ID)

    act(() => markClientSettingsUnavailable())
    fireEvent.click(row("Laptop"))
    expect(getDisplayProfiles().active.name).toBe("Laptop")
  })

  it("shows the list with no editor, and a tap on a row picks that profile for this browser", () => {
    renderWithThreeProfiles()
    expect(screen.queryByRole("region")).toBeNull()

    fireEvent.click(row("Phone"))

    expect(getDisplayProfiles().active.name).toBe("Phone")
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "Laptop", "PhoneThis browser"])
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("› on a row opens that profile in the editor in a sheet, and keeps the pick of this browser", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(within(list()).getByRole("button", { name: "Edit Phone" }))

    const sheet = screen.getByRole("dialog", { name: "Edit profile" })
    expect(within(sheet).getByRole("region", { name: "Phone" })).not.toBeNull()
    expect(within(sheet).getByRole("switch", { name: "Own sort and filters" }).getAttribute("aria-checked")).toBe("true")
    expect(getDisplayProfiles().active.name).toBe("Laptop")

    await user.click(footerClose(sheet))
    expect(screen.queryByRole("dialog")).toBeNull()

    await user.click(within(list()).getByRole("button", { name: "Edit Default" }))
    expect(within(screen.getByRole("dialog", { name: "Edit profile" })).getByRole("region", { name: "Default" })).not.toBeNull()
  })

  it("closing the editor sheet moves focus back to the › that opened it", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()

    await user.click(within(list()).getByRole("button", { name: "Edit Phone" }))
    await user.click(footerClose(screen.getByRole("dialog", { name: "Edit profile" })))

    await waitFor(() => expect(document.activeElement).toBe(within(list()).getByRole("button", { name: "Edit Phone" })))
  })

  it("the editor sheet opens with focus on the sheet, so no help tooltip opens while the controls wait for the server", async () => {
    const user = userEvent.setup()
    const laptop = createDisplayProfile("Laptop")
    setDisplayProfileOwnSortAndFilters(laptop, true)
    _resetClientSettingsForTests()
    renderSettings()

    await user.click(within(list()).getByRole("button", { name: "Edit Laptop" }))

    const sheet = screen.getByRole("dialog", { name: "Edit profile" })
    expect(document.activeElement).toBe(sheet)
    expect(screen.queryByRole("tooltip")).toBeNull()
  })

  it("a delete in the sheet closes the sheet", async () => {
    const user = userEvent.setup()
    renderWithThreeProfiles()
    await user.click(within(list()).getByRole("button", { name: "Edit Phone" }))

    await user.click(within(screen.getByRole("dialog", { name: "Edit profile" })).getByRole("button", { name: "Delete…" }))
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }))

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(within(list()).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["Default", "LaptopThis browser"])
  })
})
