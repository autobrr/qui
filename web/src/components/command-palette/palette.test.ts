/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Circle } from "lucide-react"
import { describe, expect, it } from "vitest"
import { findEntry, INITIAL_PALETTE_STATE, type PaletteEntry, type PaletteGroup, type PaletteKeyContext, paletteKeyAction, paletteReducer, searchEntries } from "./palette"

function entry(id: string, label: string, extra: Partial<PaletteEntry> = {}): PaletteEntry {
  return { id, label, icon: Circle, run: () => {}, ...extra }
}

const settings = entry("page.settings", "Settings", {
  sections: [
    { id: "general", label: "General", entries: [entry("settings.themes", "Themes", { trail: "Settings › General" })] },
    { id: "rest", entries: [entry("settings.logs", "Logs", { trail: "Settings", ownListOnly: true })] },
  ],
})
const crossSeed = entry("page.cross-seed", "Cross-Seed", {
  sections: [{ id: "sources", entries: [entry("cross-seed.rss", "RSS", { trail: "Cross-Seed" })] }],
})
const groups: PaletteGroup[] = [
  { id: "instances", entries: [entry("instance.1", "seedbox")] },
  { id: "pages", entries: [entry("page.dashboard", "Dashboard"), crossSeed, entry("page.rss", "RSS"), settings, entry("page.logs", "Logs")] },
]

const ids = (entries: PaletteEntry[]) => entries.map(found => found.id)

describe("searchEntries", () => {
  it("puts the best match first", () => {
    expect(ids(searchEntries(groups, "d")).at(0)).toBe("page.dashboard")
  })

  it("puts an entry whose label matches before an entry that matches through its trail", () => {
    expect(ids(searchEntries(groups, "seg"))).toEqual(["page.settings", "settings.themes"])
  })

  it("puts a page before a section of another page that has the same name", () => {
    expect(ids(searchEntries(groups, "rss")).slice(0, 2)).toEqual(["page.rss", "cross-seed.rss"])
  })

  it("finds a section of a page from the first level", () => {
    expect(ids(searchEntries(groups, "themes"))).toEqual(["settings.themes"])
  })

  it("finds an own-list-only entry in a search of its list", () => {
    expect(ids(searchEntries(settings.sections ?? [], "logs"))).toEqual(["settings.logs"])
  })

  it("leaves an own-list-only entry out of a search from the first level, so one destination gives one row", () => {
    expect(ids(searchEntries(groups, "logs"))).toEqual(["page.logs"])
  })
})

describe("findEntry", () => {
  it("finds a section inside a page", () => {
    expect(findEntry(groups, "settings.themes")?.label).toBe("Themes")
  })
})

describe("paletteReducer", () => {
  it("moves the highlight back to the first row when the text changes", () => {
    const picked = paletteReducer(INITIAL_PALETTE_STATE, { type: "highlighted", id: "page.settings" })

    expect(paletteReducer(picked, { type: "typed", query: "s" })).toEqual({ query: "s", parentId: null, highlightId: null })
  })

  it("opens the sections of an entry with an empty search and the highlight on the first row", () => {
    const typed = paletteReducer(INITIAL_PALETTE_STATE, { type: "typed", query: "sett" })
    const picked = paletteReducer(typed, { type: "highlighted", id: "page.settings" })

    expect(paletteReducer(picked, { type: "sectionsOpened", parentId: "page.settings" })).toEqual({ query: "", parentId: "page.settings", highlightId: null })
  })

  it("goes back to the first level with the highlight on the entry that the user came from", () => {
    const inSections = paletteReducer(INITIAL_PALETTE_STATE, { type: "sectionsOpened", parentId: "page.settings" })
    const searched = paletteReducer(inSections, { type: "typed", query: "the" })

    expect(paletteReducer(searched, { type: "wentBack" })).toEqual({ query: "", parentId: null, highlightId: "page.settings" })
  })
})

describe("paletteKeyAction", () => {
  const press = (key: string, modifiers: { ctrlKey?: boolean; metaKey?: boolean; shiftKey?: boolean; isComposing?: boolean; keyCode?: number } = {}) => ({
    key, code: "", keyCode: 0, isComposing: false, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...modifiers,
  })
  const secondInstance = entry("instance.2", "home-nas", { shortcut: "2" })
  const torrentSearch = entry("torrent-search", "Search torrents", { shortcut: "Enter" })
  const context: PaletteKeyContext = { entries: [secondInstance, torrentSearch], moreOf: undefined, inSections: false, queryEmpty: false, caretAtEnd: true }
  const inSections: PaletteKeyContext = { ...context, inSections: true, queryEmpty: true }

  it("runs the torrent search on Enter with the command modifier of the platform", () => {
    expect(paletteKeyAction(press("Enter", { ctrlKey: true }), context, false)).toEqual({ type: "run", entry: torrentSearch })
    expect(paletteKeyAction(press("Enter", { metaKey: true }), context, true)).toEqual({ type: "run", entry: torrentSearch })
  })

  it("runs the torrent search on Ctrl+Enter on macOS too", () => {
    expect(paletteKeyAction(press("Enter", { ctrlKey: true }), context, true)).toEqual({ type: "run", entry: torrentSearch })
  })

  it("leaves Enter without a modifier to cmdk, which opens the highlighted row", () => {
    expect(paletteKeyAction(press("Enter"), context, false)).toBeNull()
  })

  it("keeps Ctrl+Enter from cmdk when no torrent search can run", () => {
    expect(paletteKeyAction(press("Enter", { ctrlKey: true }), { ...context, entries: [secondInstance] }, false)).toEqual({ type: "nothing" })
  })

  it("runs the entry that has the digit as its shortcut, only with the command modifier of the platform", () => {
    expect(paletteKeyAction(press("2", { ctrlKey: true }), context, false)).toEqual({ type: "run", entry: secondInstance })
    expect(paletteKeyAction(press("2", { ctrlKey: true }), context, true)).toBeNull()
  })

  it("leaves a shortcut of no entry to the browser", () => {
    expect(paletteKeyAction(press("7", { ctrlKey: true }), context, false)).toBeNull()
  })

  it("opens the sections of the highlighted entry on ArrowRight, when the caret is at the end of the text", () => {
    expect(paletteKeyAction(press("ArrowRight"), { ...context, moreOf: settings }, false)).toEqual({ type: "openSections", parent: settings })
    expect(paletteKeyAction(press("ArrowRight"), { ...context, moreOf: settings, caretAtEnd: false }, false)).toBeNull()
    expect(paletteKeyAction(press("ArrowRight", { shiftKey: true }), { ...context, moreOf: settings }, false)).toBeNull()
  })

  it("goes back from the sections on Backspace, when the text is empty", () => {
    expect(paletteKeyAction(press("Backspace"), inSections, false)).toEqual({ type: "goBack" })
    expect(paletteKeyAction(press("Backspace"), { ...inSections, queryEmpty: false }, false)).toBeNull()
    expect(paletteKeyAction(press("Backspace"), { ...inSections, inSections: false }, false)).toBeNull()
  })

  it("leaves a key of an input method alone", () => {
    expect(paletteKeyAction(press("Backspace", { isComposing: true }), inSections, false)).toBeNull()
    expect(paletteKeyAction(press("Backspace", { keyCode: 229 }), inSections, false)).toBeNull()
  })
})
