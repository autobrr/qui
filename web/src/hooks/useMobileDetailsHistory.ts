/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRouter, useNavigate, useRouter } from "@tanstack/react-router"
import { useCallback, useEffect, useRef } from "react"

type SearchUpdate = (prev: Record<string, unknown>) => Record<string, unknown>

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
  urlHash: string | undefined
  shownHash: string | undefined | null
  onClosed: () => void
}

export function useMobileDetailsHistory({ enabled, urlHash, shownHash, onClosed }: Options) {
  const router = useRouter()
  const navigate = useNavigate()
  const ownedHash = useRef<string | undefined>(undefined)

  useEffect(() => {
    if (!enabled) return
    if (urlHash) {
      if (shownHash === urlHash) ownedHash.current = urlHash
      return
    }
    if (shownHash && ownedHash.current === shownHash) {
      ownedHash.current = undefined
      onClosed()
    }
  }, [enabled, urlHash, shownHash, onClosed])

  const closeInPlace = useCallback(() => {
    ownedHash.current = undefined
    onClosed()
    void navigate({ to: ".", search: withoutDetails, replace: true })
  }, [navigate, onClosed])

  const open = useCallback((hash: string, search: SearchUpdate) => {
    const index = currentIndex(router)
    const depth = readDepth(index, urlHash) + 1
    sessionStorage.setItem(depthKey(index + 1), `${depth}:${hash}`)
    void navigate({ to: ".", search })
  }, [navigate, router, urlHash])

  const close = useCallback(() => {
    if (!ownedHash.current) {
      onClosed()
      return
    }
    const depth = enabled ? readDepth(currentIndex(router), urlHash) : 0
    if (depth === 0) {
      closeInPlace()
      return
    }
    const unsubscribe = router.history.subscribe(({ location }) => {
      unsubscribe()
      if (new URLSearchParams(location.search).has("torrent")) closeInPlace()
    })
    router.history.go(-depth)
  }, [closeInPlace, enabled, onClosed, router, urlHash])

  return { open, close }
}
