/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef, useState } from "react"

export function useDelayedVisibility(delayMs: number) {
  const [isHidden, setIsHidden] = useState(() => {
    if (typeof document === "undefined") {
      return false
    }

    return document.hidden
  })
  const [isHiddenDelayed, setIsHiddenDelayed] = useState(() => {
    if (typeof document === "undefined") {
      return false
    }

    return document.hidden
  })
  const [isVisibleDelayed, setIsVisibleDelayed] = useState(() => {
    if (typeof document === "undefined") {
      return true
    }

    return !document.hidden
  })
  const timeoutRef = useRef<number | null>(null)

  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    const clearPending = () => {
      if (timeoutRef.current !== null) {
        window.clearTimeout(timeoutRef.current)
        timeoutRef.current = null
      }
    }

    const schedule = () => {
      clearPending()

      const hidden = document.hidden
      setIsHidden(hidden)

      if (!hidden) {
        setIsHiddenDelayed(false)
        setIsVisibleDelayed(true)
        return
      }

      setIsHiddenDelayed(false)
      setIsVisibleDelayed(false)

      timeoutRef.current = window.setTimeout(() => {
        if (document.hidden) {
          setIsHiddenDelayed(true)
          setIsVisibleDelayed(false)
        } else {
          setIsHiddenDelayed(false)
          setIsVisibleDelayed(true)
        }
        timeoutRef.current = null
      }, delayMs)
    }

    const handleVisibilityChange = () => {
      schedule()
    }

    document.addEventListener("visibilitychange", handleVisibilityChange)

    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange)
      clearPending()
    }
  }, [delayMs])

  return { isHidden, isHiddenDelayed, isVisibleDelayed }
}
