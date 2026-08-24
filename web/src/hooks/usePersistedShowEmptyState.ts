/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

/**
 * Persists the show/hide empty items state.
 * Used for toggling visibility of empty statuses, categories, and tags in the filter sidebar.
 */
export function usePersistedShowEmptyState(key: string, defaultValue: boolean = false) {
  return useClientSetting<boolean>(`qui-show-empty-${key}`, {
    defaultValue,
    parse: parseJsonBoolean,
  })
}
