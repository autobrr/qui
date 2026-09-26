/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-stretch-name-column"

/**
 * Whether the Name column stretches to fill the table (true) or keeps a fixed
 * width so the table side-scrolls (false). Stored per instance (0 is the
 * all-instances view, matching the other column hooks); defaults to stretch.
 */
export function usePersistedStretchMode(instanceId: number): [boolean, () => void] {
  const [stretch, setStretch] = useClientSetting<boolean>(`${BASE_STORAGE_KEY}:${instanceId}`, { defaultValue: true, parse: parseJsonBoolean })
  const toggle = useCallback(() => setStretch((current) => !current), [setStretch])
  return [stretch, toggle]
}
