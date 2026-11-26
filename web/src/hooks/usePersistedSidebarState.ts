/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

export function usePersistedSidebarIconOnly() {
  return useClientSetting<boolean>("qui-sidebar-icon-only", {
    defaultValue: false,
    parse: parseJsonBoolean,
  })
}

export function usePersistedSidebarHidden() {
  return useClientSetting<boolean>("qui-sidebar-hidden", {
    defaultValue: false,
    parse: parseJsonBoolean,
  })
}
