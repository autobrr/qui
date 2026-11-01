/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRouter, useNavigate, useRouter } from "@tanstack/react-router"
import { useCallback, useEffect, useRef } from "react"

type SearchUpdate = (prev: Record<string, unknown>) => Record<string, unknown>

// The router rewrites history.state on a reload and drops extra keys, but it
// keeps __TSR_index, so the count lives in session storage under that index.
// ponytail: a count outlives its entry when Back leaves the page from a view
// (a link from another page opened it). A link that later lands on that index
// reads the old count. Bind counts to entries if this shows up.
const DEPTH_PREFIX = "qui-details-depth:"

function depthKey(index: number): string {
  return `${DEPTH_PREFIX}${index}`
}

// Back left the entries after this one: a later entry at their index must not
// read their counts
function dropCountsAfter(index: number) {
  for (let i = sessionStorage.length - 1; i >= 0; i--) {
    const key = sessionStorage.key(i)
    if (key?.startsWith(DEPTH_PREFIX) && Number(key.slice(DEPTH_PREFIX.length)) > index) sessionStorage.removeItem(key)
  }
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
  const hadUrlHash = useRef(false)

  useEffect(() => {
    if (hadUrlHash.current && !urlHash) dropCountsAfter(currentIndex(router))
    hadUrlHash.current = Boolean(urlHash)
  }, [urlHash, router])

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
    // A list entry starts a new count: Back can leave an old one on its index
    const depth = (urlHash ? Number(sessionStorage.getItem(depthKey(index))) : 0) + 1
    sessionStorage.setItem(depthKey(index + 1), String(depth))
    void navigate({ to: ".", search })
  }, [navigate, router, urlHash])

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
