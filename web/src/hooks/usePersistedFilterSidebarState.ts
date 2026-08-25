/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

export function usePersistedFilterSidebarState(defaultCollapsed: boolean = false) {
  return useClientSetting<boolean>("qui-filter-sidebar-collapsed", {
    defaultValue: defaultCollapsed,
    parse: parseJsonBoolean,
  })
}
