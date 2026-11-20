/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { hasNoModifier, hasOnlyMod, isMac, latinKeyOf } from "@/lib/keyboard"
import type { ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { defaultFilter } from "cmdk"
import type { LucideIcon } from "lucide-react"

/** One row of the palette: a page, an instance, a section, an action, or a torrent. */
export interface PaletteEntry {
  id: string
  label: string
  icon: LucideIcon | { instanceId: number; problem?: boolean }
  /** A short fact about the entry, for the second line of its row. */
  note?: string
  /** Where the entry lives, for a result list that shows it outside its page: "Settings › Connections". */
  trail?: string
  current?: boolean
  /** The key that runs the entry together with the command modifier, also when another row has the highlight: a digit for an instance, "Enter" for the torrent search. */
  shortcut?: string
  /** Only a search of its own list finds the entry. A search from the first level leaves it out, because the first level already finds its destination under another entry, or because its list is too long for the first level. */
  ownListOnly?: boolean
  /** Only on a torrent: the number of other torrents in its Copy group. */
  copies?: number
  /** Only on a theme: the colors of the small picture at the end of its row. */
  thumbnail?: ThemeThumbnailPalette
  /** The sections of a page, the actions of an instance, or the modes and themes of the Theme row, that open as a second level. */
  sections?: PaletteGroup[]
  /** The row shows, but it gets no highlight and Enter does not run it: a theme that needs a license. */
  disabled?: boolean
  /** What Enter does. Without it, Enter opens the sections of the entry. A disabled entry has neither. */
  run?: () => void
}

export interface PaletteGroup {
  id: string
  label?: string
  entries: PaletteEntry[]
}

export function findEntry(groups: PaletteGroup[], id: string | null): PaletteEntry | undefined {
  if (id === null) return undefined
  for (const group of groups) {
    for (const entry of group.entries) {
      if (entry.id === id) return entry
      const nested = entry.sections ? findEntry(entry.sections, id) : undefined
      if (nested) return nested
    }
  }
  return undefined
}

function flatten(groups: PaletteGroup[], level = 0): { entry: PaletteEntry; level: number }[] {
  return groups.flatMap(group => group.entries.flatMap(entry => [{ entry, level }, ...(entry.sections ? flatten(entry.sections, level + 1) : [])]))
}

/**
 * The entries at every level that match the query, best match first. A match on
 * the label goes before a match through the trail, and a page goes before a
 * section of another page that has the same name.
 */
export function searchEntries(groups: PaletteGroup[], query: string): PaletteEntry[] {
  return flatten(groups)
    .filter(({ entry, level }) => level === 0 || !entry.ownListOnly)
    .map(({ entry, level }) => {
      const labelScore = defaultFilter(entry.label, query)
      return { entry, level, score: labelScore > 0 ? 1 + labelScore : defaultFilter(entry.label, query, entry.trail ? [entry.trail] : []) }
    })
    .filter(scored => scored.score > 0)
    .sort((a, b) => b.score - a.score || a.level - b.level)
    .map(scored => scored.entry)
}

export interface PaletteState {
  query: string
  /** The entry whose sections the list shows. Null at the first level. */
  parentId: string | null
  /** The row that the user moved the highlight to. Null, or a row that the list no longer has, gives the first row. */
  highlightId: string | null
}

export type PaletteEvent =
  | { type: "typed"; query: string }
  | { type: "highlighted"; id: string }
  | { type: "sectionsOpened"; parentId: string }
  | { type: "wentBack" }

export const INITIAL_PALETTE_STATE: PaletteState = { query: "", parentId: null, highlightId: null }

export function paletteReducer(state: PaletteState, event: PaletteEvent): PaletteState {
  switch (event.type) {
    case "typed":
      return { ...state, query: event.query, highlightId: null }
    case "highlighted":
      return { ...state, highlightId: event.id }
    case "sectionsOpened":
      return { query: "", parentId: event.parentId, highlightId: null }
    case "wentBack":
      return { query: "", parentId: null, highlightId: state.parentId }
  }
}

export type PaletteKeyAction =
  | { type: "run"; entry: PaletteEntry }
  | { type: "openSections"; parent: PaletteEntry }
  | { type: "goBack" }
  /** The key does nothing, and cmdk does not get it. */
  | { type: "nothing" }

interface PaletteKeyEvent {
  key: string
  code: string
  keyCode: number
  isComposing: boolean
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
}

export interface PaletteKeyContext {
  /** The entries that can have a shortcut: those of the first level, and the torrent rows. */
  entries: PaletteEntry[]
  /** The highlighted entry, when it has sections that the list can open. */
  moreOf: PaletteEntry | undefined
  inSections: boolean
  queryEmpty: boolean
  caretAtEnd: boolean
}

/**
 * What a key in the search field does. Null leaves the key to cmdk and to the
 * field: the keys that move the highlight, Enter on the highlighted row, and text.
 */
export function paletteKeyAction(event: PaletteKeyEvent, context: PaletteKeyContext, mac = isMac): PaletteKeyAction | null {
  // A key that goes to an input method is not a key of the palette; cmdk skips it the same way (Safari reports keyCode 229).
  if (event.isComposing || event.keyCode === 229) return null
  const shortcut = event.key === "Enter" ? "Enter" : latinKeyOf(event)
  // On macOS, Ctrl+Enter searches the torrents as ⌘↵ does.
  if (hasOnlyMod(event, mac) || (shortcut === "Enter" && hasOnlyMod(event, false))) {
    const entry = context.entries.find(candidate => candidate.shortcut !== undefined && candidate.shortcut === shortcut)
    if (entry) return { type: "run", entry }
    // Without text there is no torrent search, and Enter with the modifier must not open the highlighted row.
    return shortcut === "Enter" ? { type: "nothing" } : null
  }
  if (!hasNoModifier(event)) return null
  if (event.key === "ArrowRight" && context.caretAtEnd && context.moreOf) return { type: "openSections", parent: context.moreOf }
  if ((event.key === "Backspace" || event.key === "ArrowLeft") && context.queryEmpty && context.inSections) return { type: "goBack" }
  return null
}
