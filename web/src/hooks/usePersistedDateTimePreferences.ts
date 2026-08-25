/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { useClientSetting } from "@/lib/client-settings"

export interface DateTimePreferences {
  timezone: string
  timeFormat: "12h" | "24h"
  dateFormat: "iso" | "us" | "eu" | "relative"
}

const DEFAULT_PREFERENCES: DateTimePreferences = {
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  timeFormat: "24h",
  dateFormat: "iso",
}

const parseDateTimePreferences = (raw: string): DateTimePreferences => ({
  ...DEFAULT_PREFERENCES,
  ...JSON.parse(raw),
})

export function usePersistedDateTimePreferences() {
  const [preferences, setStored] = useClientSetting<DateTimePreferences>("qui-datetime-preferences", {
    defaultValue: DEFAULT_PREFERENCES,
    parse: parseDateTimePreferences,
  })

  const setPreferences = useCallback(
    (newPreferences: Partial<DateTimePreferences>) => {
      setStored((prev) => ({ ...prev, ...newPreferences }))
    },
    [setStored]
  )

  return {
    preferences,
    setPreferences,
    resetToDefaults: () => setPreferences(DEFAULT_PREFERENCES),
  } as const
}
