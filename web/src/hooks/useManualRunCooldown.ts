/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useMemo, useState } from "react"

export const MIN_RSS_INTERVAL_MINUTES = 30
export const DEFAULT_RSS_INTERVAL_MINUTES = 120

function formatDurationShort(ms: number): string {
  const totalSeconds = Math.ceil(Math.max(ms, 0) / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts: string[] = []
  if (hours > 0) {
    parts.push(`${hours}h`)
  }
  parts.push(`${String(minutes).padStart(2, "0")}m`)
  parts.push(`${String(seconds).padStart(2, "0")}s`)
  return parts.join(" ")
}

/** Manual RSS runs wait one run interval after the last run. Ticks once a second while the wait is on. */
export function useManualRunCooldown(runIntervalMinutes: number, lastRunStartedAt?: string) {
  const enforcedRunIntervalMinutes = Math.max(runIntervalMinutes, MIN_RSS_INTERVAL_MINUTES)
  const [now, setNow] = useState(() => Date.now())

  const nextManualRunAt = useMemo(() => {
    if (!lastRunStartedAt) {
      return null
    }
    const startedAt = new Date(lastRunStartedAt)
    if (Number.isNaN(startedAt.getTime())) {
      return null
    }
    return new Date(startedAt.getTime() + enforcedRunIntervalMinutes * 60 * 1000)
  }, [enforcedRunIntervalMinutes, lastRunStartedAt])

  const remainingMs = nextManualRunAt ? Math.max(nextManualRunAt.getTime() - now, 0) : 0
  const active = remainingMs > 0

  useEffect(() => {
    if (!active) {
      return
    }
    const tick = () => setNow(Date.now())
    tick()
    const interval = window.setInterval(tick, 1_000)
    return () => window.clearInterval(interval)
  }, [active, nextManualRunAt])

  return {
    active,
    display: active ? formatDurationShort(remainingMs) : "",
    enforcedRunIntervalMinutes,
  }
}
