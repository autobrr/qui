/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const OPEN_EVENT = "qui-open-command-palette"

export function openCommandPalette(text = "") {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: text }))
}

export function onOpenCommandPalette(listener: (text: string) => void) {
  const onOpen = (event: Event) => listener((event as CustomEvent<string>).detail)
  window.addEventListener(OPEN_EVENT, onOpen)
  return () => window.removeEventListener(OPEN_EVENT, onOpen)
}
