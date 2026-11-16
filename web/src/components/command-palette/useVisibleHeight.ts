/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

function subscribe(onChange: () => void) {
  window.visualViewport?.addEventListener("resize", onChange)
  return () => window.visualViewport?.removeEventListener("resize", onChange)
}

/**
 * The height in px of the part of the window that an on-screen keyboard does
 * not cover. A phone browser keeps `vh` at the full height while the keyboard
 * is open. Null when the browser does not give the height.
 */
export function useVisibleHeight(): number | null {
  return useSyncExternalStore(subscribe, () => window.visualViewport?.height ?? null)
}
