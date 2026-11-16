/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// An iPad with a keyboard reports a Mac user agent, and its keyboard has ⌘.
export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)

/** The names of keys as a keyboard of the platform prints them. */
export const keyLabels = isMac ? { enter: "↵", backspace: "⌫", escape: "esc" } : { enter: "Enter", backspace: "Backspace", escape: "Esc" }

/** A shortcut with the command modifier of the platform: "⌘K" on macOS, "Ctrl+K" on Windows and Linux. */
export function modShortcut(key: string, mac = isMac): string {
  return mac ? `⌘${key}` : `Ctrl+${key}`
}

interface Modifiers {
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
}

/** True when the command modifier of the platform is the only modifier that is down: ⌘ on macOS, Ctrl on Windows and Linux. */
export function hasOnlyMod(event: Modifiers, mac = isMac): boolean {
  // AltGr reports Ctrl+Alt on Windows, and it types a character.
  if (event.altKey || event.shiftKey) return false
  return mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
}

export function hasNoModifier(event: Modifiers): boolean {
  return !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey
}

/**
 * The Latin letter or digit of a key press, in lower case, or null. A layout
 * that types no Latin letter (Cyrillic, Hangul) falls back to the position of
 * the key, as the shortcuts of the browser do.
 */
export function latinKeyOf(event: { key: string; code: string }): string | null {
  if (/^[a-z0-9]$/i.test(event.key)) return event.key.toLowerCase()
  const match = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code)
  return match ? (match[1] ?? match[2]).toLowerCase() : null
}
