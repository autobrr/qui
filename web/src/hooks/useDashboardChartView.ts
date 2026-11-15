/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

export type DashboardChartView = "day" | "live"

const SETTING = { defaultValue: "day" as DashboardChartView, parse: (raw: string): DashboardChartView => JSON.parse(raw) === "live" ? "live" : "day" }

/** The view of the Dashboard chart that the user picked last, a Client setting. */
export function useDashboardChartView(): [DashboardChartView, (value: DashboardChartView) => void] {
  return useClientSetting<DashboardChartView>("qui-dashboard-chart-view", SETTING)
}
