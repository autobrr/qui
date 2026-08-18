/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

/**
 * Subscribe to a media query and return whether it matches.
 */
export function useMediaQuery(query: string): boolean {
  const subscribe = (callback: () => void) => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return () => {}
    }

    const mediaQuery = window.matchMedia(query)

    if (typeof mediaQuery.addEventListener === "function") {
      mediaQuery.addEventListener("change", callback)
      return () => mediaQuery.removeEventListener("change", callback)
    }

    // Legacy fallback
    const legacyQuery = mediaQuery as MediaQueryList & {
      addListener?: (listener: () => void) => void
      removeListener?: (listener: () => void) => void
    }
    legacyQuery.addListener?.(callback)
    return () => legacyQuery.removeListener?.(callback)
  }

  const getSnapshot = () => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return false
    }
    return window.matchMedia(query).matches
  }

  const getServerSnapshot = () => false

  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
}

const PHONE_LANDSCAPE = "(pointer: coarse) and (max-height: 500px)"

/**
 * Returns true when the viewport is phone-sized: narrower than the md
 * breakpoint, or a touch device in short landscape. Must stay the exact
 * complement of the `desk` custom variant in index.css.
 */
export function useIsMobile(): boolean {
  return useMediaQuery(`(max-width: 767px), (${PHONE_LANDSCAPE})`)
}

/** Touch device in short landscape. Must match `phone-land` in index.css. */
export function useIsPhoneLandscape(): boolean {
  return useMediaQuery(PHONE_LANDSCAPE)
}
