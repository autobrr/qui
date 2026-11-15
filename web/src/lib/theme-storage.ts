/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

import type { Theme } from "@/config/themes"
import { isDemo } from "@/lib/demo"

export type StoredThemeMode = "light" | "dark" | "auto"

// The demo shares getqui.com with Docusaurus, which stores its own color
// mode under "theme"; the demo build mirrors that key into its own at boot.
const MODE_KEY = isDemo ? "qui-demo-theme" : "theme"
const THEME_ID_KEY = "color-theme"
// index.html reads these three before the bundle loads.
const THEME_CACHE_KEY = "theme-cache"
const CRITICAL_VARS_KEY = "theme-critical-vars"
const LIGHT_ONLY_KEY = "theme-light-only"

const VISIT_CHANGE_EVENT = "qui-default-theme-visit-changed"

/** True when the address asks for the default theme for this visit (`?theme=default`). */
export function asksForDefaultTheme(search: string): boolean {
  return new URLSearchParams(search).get("theme") === "default"
}

// Read once at page load, so navigation inside the app keeps the visit.
let visitActive = typeof window !== "undefined" && asksForDefaultTheme(window.location.search)
let visitMode: StoredThemeMode | null = null

/**
 * A default theme visit shows the default theme until the next page load
 * without `?theme=default`. A theme selection in a picker ends it.
 */
export function isDefaultThemeVisit(): boolean {
  return visitActive
}

export function endDefaultThemeVisit(): void {
  if (!visitActive) return
  visitActive = false
  visitMode = null
  window.dispatchEvent(new Event(VISIT_CHANGE_EVENT))
}

function subscribeVisit(callback: () => void): () => void {
  window.addEventListener(VISIT_CHANGE_EVENT, callback)
  return () => window.removeEventListener(VISIT_CHANGE_EVENT, callback)
}

export function useDefaultThemeVisit(): boolean {
  return useSyncExternalStore(subscribeVisit, isDefaultThemeVisit)
}

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
  } catch {
    // A full or blocked storage keeps the old value.
  }
}

/*
 * The theme data that this browser keeps: the selected theme, the light or
 * dark mode, and the boot caches. During a default theme visit
 * (`?theme=default`) each read gives no stored value and each write stores
 * nothing, so the next normal page load shows the stored theme again. A mode
 * set during the visit lives in memory until the visit ends.
 */

export function storedThemeId(): string | null {
  return visitActive ? null : read(THEME_ID_KEY)
}

export function storeThemeId(themeId: string): void {
  if (!visitActive) write(THEME_ID_KEY, themeId)
}

export function storedMode(): StoredThemeMode | null {
  if (visitActive && visitMode) return visitMode
  const mode = read(MODE_KEY)
  return mode === "light" || mode === "dark" || mode === "auto" ? mode : null
}

export function storeMode(mode: StoredThemeMode): void {
  if (visitActive) visitMode = mode
  else write(MODE_KEY, mode)
}

/** Caches the applied theme for the paint of the next page load. cssVars are the vars of the painted mode. */
export function cacheAppliedTheme(theme: Theme, cssVars: Record<string, string>): void {
  if (visitActive) return
  write(LIGHT_ONLY_KEY, theme.lightOnly ? "true" : null)
  write(THEME_CACHE_KEY, JSON.stringify({
    id: theme.id,
    name: theme.name,
    description: theme.description,
    isPremium: theme.isPremium,
    lightOnly: theme.lightOnly,
    variations: theme.variations,
    cssVars: theme.cssVars,
    isCustom: theme.isCustom,
    rawCss: theme.rawCss,
  }))
  const background = cssVars["--background"]
  if (background) {
    write(CRITICAL_VARS_KEY, JSON.stringify({ background, foreground: cssVars["--foreground"] || "" }))
  }
}

/** The JSON that cacheAppliedTheme wrote, or null. */
export function cachedThemeJson(): string | null {
  return visitActive ? null : read(THEME_CACHE_KEY)
}

/** The background color of the cached theme, or an empty string. */
export function cachedBackground(): string {
  const raw = visitActive ? null : read(CRITICAL_VARS_KEY)
  try {
    const vars = raw ? JSON.parse(raw) as { background?: string } : null
    return (vars?.background ?? "").trim()
  } catch {
    return ""
  }
}
