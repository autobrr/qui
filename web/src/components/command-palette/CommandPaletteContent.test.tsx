/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog } from "@/components/ui/dialog"
import { TooltipProvider } from "@/components/ui/tooltip"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { Circle } from "lucide-react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { CommandPalette } from "./CommandPalette"
import { CommandPaletteContent } from "./CommandPaletteContent"
import { openCommandPalette } from "./open"
import type { PaletteEntry, PaletteGroup } from "./palette"
import type { TorrentResults } from "./useTorrentResults"

const mocks = vi.hoisted(() => ({
  destinations: [] as PaletteGroup[],
  noTorrents: { entries: [], noMatch: false } as TorrentResults,
  torrentSearch: { entries: [], noMatch: false } as TorrentResults,
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", () => ({ useTranslation: () => mocks.translation }))
vi.mock("./useDestinations", () => ({ useDestinations: () => mocks.destinations }))
vi.mock("./useTorrentResults", () => ({ useTorrentResults: (query: string) => (query ? mocks.torrentSearch : mocks.noTorrents) }))

function entry(id: string, label: string, extra: Partial<PaletteEntry> = {}): PaletteEntry {
  return { id, label, icon: Circle, run: () => {}, ...extra }
}

mocks.torrentSearch.entries.push(entry("torrent-search", "Search torrents"))
mocks.destinations.push({
  id: "pages",
  entries: [
    entry("page.dashboard", "Dashboard"),
    entry("page.settings", "Settings", { sections: [{ id: "sections", entries: [entry("settings.themes", "Themes"), entry("settings.logs", "Logs")] }] }),
    entry("action.theme", "Theme", { run: undefined, sections: [{ id: "themes", entries: [entry("theme.locked", "Locked", { run: undefined, disabled: true }), entry("theme-mode.light", "Light"), entry("theme-mode.dark", "Dark")] }] }),
  ],
})

function namedRow() {
  const id = screen.getByRole("combobox").getAttribute("aria-activedescendant")
  return id === null ? null : document.getElementById(id)
}

function highlightedRow() {
  return screen.getAllByRole("option").find(row => row.getAttribute("aria-selected") === "true")
}

beforeEach(() => {
  Element.prototype.scrollIntoView = () => {}
  Element.prototype.scrollTo = () => {}
})

function openPalette() {
  render(
    <TooltipProvider>
      <Dialog open>
        <CommandPaletteContent close={() => {}} />
      </Dialog>
    </TooltipProvider>
  )
  return screen.getByRole("combobox")
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  mocks.torrentSearch.noMatch = false
})

describe("CommandPaletteContent", () => {
  it("names the first row in the search field when the palette opens", () => {
    openPalette()

    expect(namedRow()?.textContent).toBe("Dashboard")
  })

  it("names the first match after the text changes, when the highlight was on another row", () => {
    const field = openPalette()
    fireEvent.change(field, { target: { value: "s" } })
    fireEvent.keyDown(field, { key: "ArrowDown" })
    expect(highlightedRow()?.textContent).not.toBe("Settings")

    fireEvent.change(field, { target: { value: "sett" } })

    expect(namedRow()?.textContent).toBe("Settings")
    expect(highlightedRow()?.textContent).toBe("Settings")
  })

  it("puts the row that searches the torrents after the destinations, and first on a touch screen", () => {
    fireEvent.change(openPalette(), { target: { value: "dash" } })
    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Dashboard", "Search torrents"])
    cleanup()

    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query === "(pointer: coarse)", addEventListener() {}, removeEventListener() {} }))
    fireEvent.change(openPalette(), { target: { value: "dash" } })

    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Search torrents", "Dashboard"])
    expect(highlightedRow()?.textContent).toBe("Search torrents")
  })

  it("puts the destinations first on a touch screen when no torrent matches", () => {
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query === "(pointer: coarse)", addEventListener() {}, removeEventListener() {} }))
    mocks.torrentSearch.noMatch = true

    fireEvent.change(openPalette(), { target: { value: "dash" } })

    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Dashboard", "Search torrents"])
    expect(highlightedRow()?.textContent).toBe("Dashboard")
  })

  it("puts the highlight on the page after Back from its last section", () => {
    const field = openPalette()
    fireEvent.keyDown(field, { key: "ArrowDown" })
    fireEvent.keyDown(field, { key: "ArrowRight" })
    fireEvent.keyDown(field, { key: "End" })
    expect(highlightedRow()?.textContent).toBe("Logs")

    fireEvent.keyDown(field, { key: "Backspace" })

    expect(highlightedRow()?.textContent).toBe("Settings")
    expect(namedRow()?.textContent).toBe("Settings")
  })

  it("opens the sections of an entry that has no action of its own on Enter", () => {
    const field = openPalette()
    fireEvent.keyDown(field, { key: "End" })
    expect(highlightedRow()?.textContent).toBe("Theme")

    fireEvent.keyDown(field, { key: "Enter" })

    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Locked", "Light", "Dark"])
  })

  it("gives a disabled row no highlight, and a click on it runs nothing and leaves the cursor in the search field", () => {
    const field = openPalette()
    fireEvent.keyDown(field, { key: "End" })
    fireEvent.keyDown(field, { key: "Enter" })
    expect(highlightedRow()?.textContent).toBe("Light")

    fireEvent.change(field, { target: { value: "locked" } })
    expect(highlightedRow()).toBeUndefined()
    expect(namedRow()).toBeNull()

    const pressGoesToTheBrowser = fireEvent.mouseDown(screen.getByRole("option"))
    fireEvent.click(screen.getByRole("option"))

    expect(pressGoesToTheBrowser).toBe(false)
    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Locked"])
  })

  it("leaves Enter on the Close button to the button, and runs no row", () => {
    const run = vi.spyOn(mocks.destinations[0].entries[0], "run")
    openPalette()

    const enterGoesToTheBrowser = fireEvent.keyDown(screen.getByRole("button", { name: "common:actions.close" }), { key: "Enter" })

    expect(run).not.toHaveBeenCalled()
    expect(enterGoesToTheBrowser).toBe(true)
  })

  it("puts the highlight on the page after a click on Back with text in the field", () => {
    const field = openPalette()
    fireEvent.keyDown(field, { key: "ArrowDown" })
    fireEvent.keyDown(field, { key: "ArrowRight" })
    fireEvent.change(field, { target: { value: "lo" } })

    fireEvent.click(screen.getByRole("button", { name: "common:commandPalette.hints.back" }))

    expect(highlightedRow()?.textContent).toBe("Settings")
  })
})

describe("CommandPalette", () => {
  it("opens when a button asks for it", () => {
    render(<TooltipProvider><CommandPalette /></TooltipProvider>)
    expect(screen.queryByRole("combobox")).toBeNull()

    act(() => openCommandPalette())

    expect(screen.getByRole("combobox")).toBe(document.activeElement)
  })

  it("starts with the text that the button gives, selected, and searches for it", () => {
    render(<TooltipProvider><CommandPalette /></TooltipProvider>)

    act(() => openCommandPalette("dash"))

    const field = screen.getByRole<HTMLInputElement>("combobox")
    expect([field.value, field.selectionStart, field.selectionEnd]).toEqual(["dash", 0, 4])
    expect(screen.getAllByRole("option").map(row => row.textContent)).toEqual(["Dashboard", "Search torrents"])
  })
})
