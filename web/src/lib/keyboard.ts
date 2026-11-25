/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)

export const keyLabels = isMac ? { enter: "↵", backspace: "⌫", escape: "esc" } : { enter: "Enter", backspace: "Backspace", escape: "Esc" }

export function modShortcut(key: string, mac = isMac): string {
  return mac ? `⌘${key}` : `Ctrl+${key}`
}

interface Modifiers {
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
}

export function hasOnlyMod(event: Modifiers, mac = isMac): boolean {
  if (event.altKey || event.shiftKey) return false
  return mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
}

export function hasNoModifier(event: Modifiers): boolean {
  return !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey
}

export function latinKeyOf(event: { key: string; code: string }): string | null {
  if (/^[a-z0-9]$/i.test(event.key)) return event.key.toLowerCase()
  const match = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code)
  return match ? (match[1] ?? match[2]).toLowerCase() : null
}
