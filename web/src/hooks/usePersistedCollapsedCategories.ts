/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

const parseCollapsedCategories = (raw: string): Set<string> => {
  const parsed = JSON.parse(raw)
  if (Array.isArray(parsed) && parsed.every(item => typeof item === "string")) {
    return new Set(parsed)
  }
  throw new Error("invalid collapsed categories")
}

const serializeCollapsedCategories = (value: Set<string>): string => JSON.stringify(Array.from(value))

const NO_COLLAPSED = new Set<string>()

export function usePersistedCollapsedCategories(instanceId: number) {
  return useClientSetting<Set<string>>(`qui-collapsed-categories-${instanceId}`, {
    defaultValue: NO_COLLAPSED,
    parse: parseCollapsedCategories,
    serialize: serializeCollapsedCategories,
  })
}
