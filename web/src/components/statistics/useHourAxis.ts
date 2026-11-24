/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useIsPhone } from "@/hooks/useMediaQuery"
import { hourAxis, type ChartUnit, type HourAxis, type StatisticsBar } from "@/lib/statistics"
import { useMemo } from "react"

export function useHourAxis(bars: Pick<StatisticsBar, "start" | "width">[], unit: ChartUnit): HourAxis | undefined {
  const narrow = useIsPhone()
  return useMemo(() => unit === "hour" ? hourAxis(bars, narrow) : undefined, [bars, unit, narrow])
}
