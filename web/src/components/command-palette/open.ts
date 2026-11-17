/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const OPEN_EVENT = "qui-open-command-palette"

/** Opens the palette from a button, for a device without a keyboard. The search field starts with `text`, selected. */
export function openCommandPalette(text = "") {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: text }))
}

/** Calls the listener with the text each time a button asks for the palette. Returns the function that stops the calls. */
export function onOpenCommandPalette(listener: (text: string) => void) {
  const onOpen = (event: Event) => listener((event as CustomEvent<string>).detail)
  window.addEventListener(OPEN_EVENT, onOpen)
  return () => window.removeEventListener(OPEN_EVENT, onOpen)
}
