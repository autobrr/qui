/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useRef, useState } from "react"

type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: "accepted" | "dismissed"; platform: string }>
}

const SUPPRESS_UNTIL_KEY = "pwa-install-suppress-until"
const SUPPRESS_FOREVER_KEY = "pwa-install-suppress-forever"
const SUPPRESS_WINDOW_MS = 1000 * 60 * 60 * 24 // 24 hours

const isStandalone = () => typeof window !== "undefined" && window.matchMedia("(display-mode: standalone)").matches
const isLikelyMobile = () => {
  const uaData =
    typeof navigator !== "undefined"
      ? (navigator as Navigator & { userAgentData?: { mobile?: boolean } }).userAgentData
      : undefined

  if (uaData?.mobile !== undefined) {
    return uaData.mobile
  }

  if (typeof window !== "undefined") {
    return window.matchMedia("(pointer: coarse)").matches
  }

  return false
}

export function usePWAInstallPrompt() {
  const [promptEvent, setPromptEvent] = useState<BeforeInstallPromptEvent | null>(null)
  const [memorySuppressUntil, setMemorySuppressUntil] = useState(0)
  const [suppressForever, setSuppressForever] = useState(false)

  const readSuppressUntil = useCallback(() => {
    try {
      const forever = localStorage.getItem(SUPPRESS_FOREVER_KEY)
      if (forever === "1") {
        setSuppressForever(true)
        return Number.POSITIVE_INFINITY
      }
    } catch {
      // Ignore storage failures
    }

    let suppressUntil = memorySuppressUntil
    try {
      const stored = Number(localStorage.getItem(SUPPRESS_UNTIL_KEY) || 0)
      if (!Number.isNaN(stored)) {
        suppressUntil = Math.max(suppressUntil, stored)
      }
    } catch {
      // localStorage may be unavailable (e.g. private browsing); fall back to in-memory value
    }
    return suppressUntil
  }, [memorySuppressUntil])

  const writeSuppressUntil = useCallback((until: number) => {
    setMemorySuppressUntil(until)
    try {
      localStorage.setItem(SUPPRESS_UNTIL_KEY, String(until))
    } catch {
      // Ignore persistence failures; session-level suppression still applies
    }
  }, [])

  const setSuppressForeverFlag = useCallback(() => {
    setSuppressForever(true)
    try {
      localStorage.setItem(SUPPRESS_FOREVER_KEY, "1")
    } catch {
      // Ignore persistence failures
    }
    setPromptEvent(null)
  }, [])

  useEffect(() => {
    const handleBeforeInstall = (event: Event) => {
      const suppressUntil = readSuppressUntil()
      if (suppressForever || suppressUntil > Date.now()) return
      if (isStandalone()) return
      if (!isLikelyMobile()) return

      event.preventDefault()
      setPromptEvent(event as BeforeInstallPromptEvent)
    }

    window.addEventListener("beforeinstallprompt", handleBeforeInstall)

    return () => {
      window.removeEventListener("beforeinstallprompt", handleBeforeInstall)
    }
  }, [readSuppressUntil, suppressForever])

  const suppressFor = useCallback((durationMs = SUPPRESS_WINDOW_MS) => {
    const until = Date.now() + durationMs
    writeSuppressUntil(until)
    setPromptEvent(null)
  }, [writeSuppressUntil])

  const promptInstall = useCallback(async () => {
    if (!promptEvent) return false

    await promptEvent.prompt()
    const result = await promptEvent.userChoice
    setPromptEvent(null)

    return result.outcome === "accepted"
  }, [promptEvent])

  useEffect(() => {
    const handleInstalled = () => {
      setSuppressForeverFlag()
    }

    window.addEventListener("appinstalled", handleInstalled)
    return () => window.removeEventListener("appinstalled", handleInstalled)
  }, [setSuppressForeverFlag])

  return {
    promptAvailable: !!promptEvent && !suppressForever,
    shouldShowPrompt: !!promptEvent && !suppressForever,
    promptInstall,
    suppressFor,
    suppressForever: setSuppressForeverFlag,
  }
}
