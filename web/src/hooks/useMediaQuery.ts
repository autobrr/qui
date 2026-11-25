/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

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

export const NARROW_PHONE_QUERY = "(max-width: 639px)"
export const SIDEWAYS_PHONE_QUERY = "(pointer: coarse) and (max-height: 499px) and (max-width: 1023px)"

export function useIsPhone(): boolean {
  const narrow = useMediaQuery(NARROW_PHONE_QUERY)
  const sideways = useMediaQuery(SIDEWAYS_PHONE_QUERY)
  return narrow || sideways
}

export function useIsMobile(): boolean {
  return useMediaQuery("(max-width: 767px)")
}
