/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HISTORY_WRITE_MS, presetPeriod, type StatisticsPeriod } from "@/lib/statistics"
import { useEffect, useState } from "react"

export function useLastDayPeriod(): StatisticsPeriod {
  const [period, setPeriod] = useState(() => presetPeriod("24h", new Date()))
  useEffect(() => {
    const move = () => {
      if (document.visibilityState !== "visible") {
        return
      }
      setPeriod(current => Date.now() - current.to.getTime() < HISTORY_WRITE_MS ? current : presetPeriod("24h", new Date()))
    }
    const timer = setInterval(move, 60_000)
    document.addEventListener("visibilitychange", move)
    return () => {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", move)
    }
  }, [])
  return period
}
