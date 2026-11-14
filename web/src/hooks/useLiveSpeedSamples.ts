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
 * Takes a sample of the speed of each instance that answers, every 2 seconds while the caller is mounted. It starts
 * empty and keeps the last 3 minutes. While the page is hidden, the stream sends no speeds, so the samples start again.
 */
export function useLiveSpeedSamples(statsData: DashboardInstanceStats[]): LiveSample[] {
  const latest = useRef(statsData)
  useEffect(() => {
    latest.current = statsData
  }, [statsData])
  const [samples, setSamples] = useState<LiveSample[]>([])

  useEffect(() => {
    const sample = () => {
      if (document.visibilityState !== "visible") {
        setSamples(previous => previous.length > 0 ? [] : previous)
        return
      }
      const speeds = new Map(latest.current.filter(d => instanceHealth(d) === "ok").map(d => [d.instance.id, { up: d.stats?.totalUploadSpeed ?? 0, down: d.stats?.totalDownloadSpeed ?? 0 }]))
      setSamples(previous => [...previous.slice(-(LIVE_SAMPLES - 1)), { time: Date.now(), speeds }])
    }
    const timer = setInterval(sample, LIVE_SAMPLE_MS)
    return () => clearInterval(timer)
  }, [])

  return samples
}
