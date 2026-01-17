/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { router } from "@/router"

export const LAST_ONLINE_PATH_KEY = "pwa-last-online-path"

const rememberCurrentPath = () => {
  const path = (() => {
    const href = router.state.location.href
    if (href) return href
    if (typeof window !== "undefined") {
      return `${window.location.pathname}${window.location.search}${window.location.hash}`
    }
    return "/"
  })()

  try {
    sessionStorage.setItem(LAST_ONLINE_PATH_KEY, path)
  } catch {
    // Ignore storage failures
  }

  return path
}

const readLastOnlinePath = () => {
  try {
    const stored = sessionStorage.getItem(LAST_ONLINE_PATH_KEY) || "/"
    return stored === "/offline" ? "/" : stored
  } catch {
    return "/"
  }
}

const ensureFallbackPath = () => {
  try {
    if (!sessionStorage.getItem(LAST_ONLINE_PATH_KEY)) {
      sessionStorage.setItem(LAST_ONLINE_PATH_KEY, "/")
    }
  } catch {
    // Ignore storage failures
  }
}

const navigateTo = (target: string) => {
  try {
    router.navigate({ to: target as any })
  } catch {
    router.navigate({ to: "/" as any })
  }
}

export function useOfflineRedirect() {
  useEffect(() => {
    const redirectOffline = () => {
      if (typeof navigator === "undefined") return
      if (!navigator.onLine) {
        if (router.state.location.pathname !== "/offline") {
          rememberCurrentPath()
          router.navigate({ to: "/offline" })
        } else {
          // If we booted on /offline, ensure we have some fallback path stored
          ensureFallbackPath()
        }
      }
    }

    const redirectOnline = () => {
      if (typeof navigator === "undefined") return
      if (navigator.onLine && router.state.location.pathname === "/offline") {
        const target = readLastOnlinePath()
        navigateTo(target)
      }
    }

    redirectOffline()
    redirectOnline()
    window.addEventListener("offline", redirectOffline)
    window.addEventListener("online", redirectOnline)

    return () => {
      window.removeEventListener("offline", redirectOffline)
      window.removeEventListener("online", redirectOnline)
    }
  }, [])
}
