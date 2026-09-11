/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { useClientSetting } from "@/lib/client-settings"

const BASE_STORAGE_KEY = "qui-stretch-name-column"

function parseStretch(raw: string): boolean {
  return raw !== "false"
}

/**
 * Whether the Name column stretches to fill the table (true) or keeps a fixed
 * width so the table side-scrolls (false). Stored per instance; defaults to stretch.
 */
export function usePersistedStretchMode(instanceId: number): [boolean, () => void] {
  const key = instanceId > 0 ? `${BASE_STORAGE_KEY}:${instanceId}` : BASE_STORAGE_KEY
  const [stretch, setStretch] = useClientSetting<boolean>(key, { defaultValue: true, parse: parseStretch })
  const toggle = useCallback(() => setStretch((current) => !current), [setStretch])
  return [stretch, toggle]
}
