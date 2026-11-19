/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

function parse(raw: string): number[] {
  const value: unknown = JSON.parse(raw)
  return Array.isArray(value) ? value.filter((id): id is number => Number.isInteger(id)) : []
}

const SETTING = { defaultValue: [] as number[], parse }

/** The ids of the instances whose rows are open on the Dashboard, a Display setting. */
export function useDashboardOpenInstances(): [number[], (value: number[] | ((prev: number[]) => number[])) => void] {
  return useClientSetting<number[]>("qui-dashboard-open-instances", SETTING)
}
