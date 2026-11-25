/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { hasNoModifier, hasOnlyMod, isMac, latinKeyOf } from "@/lib/keyboard"
import type { ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { defaultFilter } from "cmdk"
import type { LucideIcon } from "lucide-react"

export interface PaletteEntry {
  id: string
  label: string
  icon: LucideIcon | { instanceId: number; problem?: boolean }
  note?: string
  trail?: string
  current?: boolean
  shortcut?: string
  ownListOnly?: boolean
  copies?: number
  thumbnail?: ThemeThumbnailPalette
  sections?: PaletteGroup[]
  disabled?: boolean
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
  parentId: string | null
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
  entries: PaletteEntry[]
  moreOf: PaletteEntry | undefined
  inSections: boolean
  queryEmpty: boolean
  caretAtEnd: boolean
}

export function paletteKeyAction(event: PaletteKeyEvent, context: PaletteKeyContext, mac = isMac): PaletteKeyAction | null {
  if (event.isComposing || event.keyCode === 229) return null
  const shortcut = event.key === "Enter" ? "Enter" : latinKeyOf(event)
  if (hasOnlyMod(event, mac) || (shortcut === "Enter" && hasOnlyMod(event, false))) {
    const entry = context.entries.find(candidate => candidate.shortcut !== undefined && candidate.shortcut === shortcut)
    if (entry) return { type: "run", entry }
    return shortcut === "Enter" ? { type: "nothing" } : null
  }
  if (!hasNoModifier(event)) return null
  if (event.key === "ArrowRight" && context.caretAtEnd && context.moreOf) return { type: "openSections", parent: context.moreOf }
  if ((event.key === "Backspace" || event.key === "ArrowLeft") && context.queryEmpty && context.inSections) return { type: "goBack" }
  return null
}
