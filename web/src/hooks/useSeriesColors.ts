/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"
import { SERIES_COLORS } from "@/lib/statistics"

export type SeriesColorSet = "distinct" | "theme"

export const SERIES_COLOR_SETS: SeriesColorSet[] = ["distinct", "theme"]

const THEME_COLORS = [1, 2, 3, 4, 5].map(index => `var(--chart-${index})`)
const THEME_SET = { light: THEME_COLORS, dark: THEME_COLORS }

const SETTING = { defaultValue: "distinct" as SeriesColorSet, parse: (raw: string): SeriesColorSet => JSON.parse(raw) === "theme" ? "theme" : "distinct" }

/** The series color set of the Statistics page, a Client setting. */
export function useSeriesColorSet(): [SeriesColorSet, (value: SeriesColorSet) => void] {
  return useClientSetting<SeriesColorSet>("qui-statistics-series-colors", SETTING)
}

/** The colors of the series of the Statistics charts, in series order, for the light and the dark theme. */
export function useSeriesColors(): { light: string[]; dark: string[] } {
  const [set] = useSeriesColorSet()
  return set === "theme" ? THEME_SET : SERIES_COLORS
}
