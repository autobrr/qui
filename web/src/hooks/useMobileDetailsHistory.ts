/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRouter, useNavigate, useRouter } from "@tanstack/react-router"
import { useCallback, useEffect, useRef } from "react"

type SearchUpdate = (prev: Record<string, unknown>) => Record<string, unknown>

// The router rewrites history.state on a reload and drops extra keys, but it
// keeps __TSR_index, so the count lives in session storage under that index.
// ponytail: a count can outlive its entry when the user leaves with Back; it is
// read only while the URL holds a torrent, and the next open overwrites it.
function depthKey(index: number): string {
  return `qui-details-depth:${index}`
}

function currentIndex(router: AnyRouter): number {
  return router.history.location.state.__TSR_index
}

interface Options {
  enabled: boolean
  // The torrent hash in the URL
  urlHash: string | undefined
  // The torrent hash that the details view shows
  shownHash: string | undefined | null
  // Hides the view: Back or a close removed its URL
  onClosed: () => void
}

/**
 * Gives the mobile details view one history entry for each open, so Back
 * closes it. A close goes back over the entries that the view pushed, so the
 * next Back leaves the page instead of landing on a copy of the list.
 */
export function useMobileDetailsHistory({ enabled, urlHash, shownHash, onClosed }: Options) {
  const router = useRouter()
  const navigate = useNavigate()
  // The hash of the entry the view owns, so Back closes only that view
  const ownedHash = useRef<string | undefined>(undefined)

  useEffect(() => {
    if (!enabled) return
    if (urlHash) {
      // Right after a close that replaces the URL, the URL holds the old hash
      // until the replace lands. Recording it then makes the next open of the
      // same torrent close itself.
      if (shownHash === urlHash) ownedHash.current = urlHash
      return
    }
    if (shownHash && ownedHash.current === shownHash) {
      ownedHash.current = undefined
      onClosed()
    }
  }, [enabled, urlHash, shownHash, onClosed])

  const open = useCallback((search: SearchUpdate) => {
    const index = currentIndex(router)
    const depth = Number(sessionStorage.getItem(depthKey(index))) + 1
    sessionStorage.setItem(depthKey(index + 1), String(depth))
    void navigate({ to: ".", search })
  }, [navigate, router])

  const close = useCallback(() => {
    const key = depthKey(currentIndex(router))
    const depth = Number(sessionStorage.getItem(key))
    sessionStorage.removeItem(key)
    if (depth > 0 && ownedHash.current) {
      // The Back handler above closes the view when the URL loses the hash
      router.history.go(-depth)
      return
    }
    onClosed()
    if (ownedHash.current) {
      ownedHash.current = undefined
      void navigate({ to: ".", search: (prev: Record<string, unknown>) => ({ ...prev, torrent: undefined, tab: undefined, instance: undefined }), replace: true })
    }
  }, [navigate, onClosed, router])

  return { open, close }
}
