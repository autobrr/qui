/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

let open = false
const listeners = new Set<() => void>()

/** Opens or closes the one support dialog of the app, from any place in it. */
export function setSupportOpen(next: boolean) {
  open = next
  for (const listener of listeners) listener()
}

export const openSupport = () => setSupportOpen(true)

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useSupportOpen(): boolean {
  return useSyncExternalStore(subscribe, () => open)
}
