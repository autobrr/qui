/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"
import { PERIOD_PRESETS, type PeriodPreset } from "@/lib/statistics"

const SETTING = {
  defaultValue: "30d" as PeriodPreset,
  parse: (raw: string): PeriodPreset => {
    const value = JSON.parse(raw)
    return PERIOD_PRESETS.includes(value) ? value : "30d"
  },
}

export function useStatisticsPeriod(): [PeriodPreset, (value: PeriodPreset) => void] {
  return useClientSetting<PeriodPreset>("qui-statistics-period", SETTING)
}
