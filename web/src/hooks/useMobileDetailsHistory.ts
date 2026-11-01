/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRouter, useNavigate, useRouter } from "@tanstack/react-router"
import { useCallback, useEffect, useRef } from "react"

type SearchUpdate = (prev: Record<string, unknown>) => Record<string, unknown>

// The router rewrites history.state on a reload and drops extra keys, but it
// keeps __TSR_index, so the count lives in session storage under that index,
// with the torrent of its entry. Back keeps the counts, so Forward still finds
// them. A later entry at the same index reads a count only for its own torrent.
// ponytail: a link to the same torrent that later lands on that index reads the
// old count. Bind counts to history entries if this shows up.
function depthKey(index: number): string {
  return `qui-details-depth:${index}`
}

function readDepth(index: number, hash: string | undefined): number {
  const [depth, owner] = (sessionStorage.getItem(depthKey(index)) ?? "").split(":")
  return hash && owner === hash ? Number(depth) : 0
}

const withoutDetails = (prev: Record<string, unknown>) => ({ ...prev, torrent: undefined, tab: undefined, instance: undefined })

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
  // The hash that a close leaves while it goes back over the pushed entries
  const closing = useRef<string | undefined>(undefined)

  useEffect(() => {
    if (!enabled) return
    if (urlHash && closing.current && urlHash !== closing.current) {
      // The close went back to a view that a link opened: close it in place
      closing.current = undefined
      ownedHash.current = undefined
      onClosed()
      void navigate({ to: ".", search: withoutDetails, replace: true })
      return
    }
    if (urlHash) {
      // Right after a close that replaces the URL, the URL holds the old hash
      // until the replace lands. Recording it then makes the next open of the
      // same torrent close itself.
      if (shownHash === urlHash) ownedHash.current = urlHash
      return
    }
    closing.current = undefined
    if (shownHash && ownedHash.current === shownHash) {
      ownedHash.current = undefined
      onClosed()
    }
  }, [enabled, urlHash, shownHash, onClosed, navigate])

  const open = useCallback((hash: string, search: SearchUpdate) => {
    const index = currentIndex(router)
    const depth = readDepth(index, urlHash) + 1
    sessionStorage.setItem(depthKey(index + 1), `${depth}:${hash}`)
    void navigate({ to: ".", search })
  }, [navigate, router, urlHash])

  const close = useCallback(() => {
    const depth = readDepth(currentIndex(router), urlHash)
    if (depth > 0 && ownedHash.current) {
      // The effect above closes the view when the URL loses the hash
      closing.current = urlHash
      router.history.go(-depth)
      return
    }
    onClosed()
    if (ownedHash.current) {
      ownedHash.current = undefined
      void navigate({ to: ".", search: withoutDetails, replace: true })
    }
  }, [navigate, onClosed, router, urlHash])

  return { open, close }
}
