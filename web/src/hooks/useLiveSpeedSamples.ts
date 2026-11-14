/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { instanceHealth, type DashboardInstanceStats, type LiveSample } from "@/lib/dashboard-overview"
import { useEffect, useRef, useState } from "react"

export const LIVE_SAMPLE_MS = 2000

// Three minutes of samples.
export const LIVE_SAMPLES = 90

/**
 * Takes a sample of the speed of each instance that answers with fresh numbers, every 2 seconds while the caller is mounted. It starts
 * empty and keeps the last 3 minutes. While the page is hidden, the stream sends no speeds, so the samples start again.
 */
export function useLiveSpeedSamples(statsData: DashboardInstanceStats[]): LiveSample[] {
  const latest = useRef(statsData)
  useEffect(() => {
    latest.current = statsData
  }, [statsData])
  const [samples, setSamples] = useState<LiveSample[]>([])

  useEffect(() => {
    const clear = () => setSamples(previous => previous.length > 0 ? [] : previous)
    const sample = () => {
      if (document.visibilityState !== "visible") {
        clear()
        return
      }
      const speeds = new Map(latest.current.filter(d => instanceHealth(d) === "ok" && !d.streamError && !d.error).map(d => [d.instance.id, { up: d.stats?.totalUploadSpeed ?? 0, down: d.stats?.totalDownloadSpeed ?? 0 }]))
      const now = Date.now()
      // A browser can pause the timer of a hidden tab, so a sample from before the pause can be older than the window.
      setSamples(previous => [...previous.filter(old => now - old.time < LIVE_SAMPLES * LIVE_SAMPLE_MS).slice(-(LIVE_SAMPLES - 1)), { time: now, speeds }])
    }
    const onVisibility = () => {
      if (document.visibilityState !== "visible") {
        clear()
      }
    }
    const timer = setInterval(sample, LIVE_SAMPLE_MS)
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [])

  return samples
}
