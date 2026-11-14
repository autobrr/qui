/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

const CHANGE_EVENT = "qui-default-theme-visit-changed"

/** True when the address asks for the default theme for this visit (`?theme=default`). */
export function asksForDefaultTheme(search: string): boolean {
  return new URLSearchParams(search).get("theme") === "default"
}

// Read once at page load, so navigation inside the app keeps the visit.
let active = typeof window !== "undefined" && asksForDefaultTheme(window.location.search)

/**
 * A default theme visit shows the default theme until the next page load
 * without `?theme=default`. It ignores and writes no theme slot, local theme,
 * or theme cache. A theme selection in a picker ends it.
 */
export function isDefaultThemeVisit(): boolean {
  return active
}

export function endDefaultThemeVisit(): void {
  if (!active) return
  active = false
  window.dispatchEvent(new Event(CHANGE_EVENT))
}

function subscribe(callback: () => void): () => void {
  window.addEventListener(CHANGE_EVENT, callback)
  return () => window.removeEventListener(CHANGE_EVENT, callback)
}

export function useDefaultThemeVisit(): boolean {
  return useSyncExternalStore(subscribe, isDefaultThemeVisit)
}

export function _setDefaultThemeVisitForTests(value: boolean): void {
  active = value
  window.dispatchEvent(new Event(CHANGE_EVENT))
}
