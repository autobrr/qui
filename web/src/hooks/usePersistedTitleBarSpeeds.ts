/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

const STORAGE_KEY = "qui-titlebar-speeds-enabled"

export function usePersistedTitleBarSpeeds(defaultValue: boolean = false) {
  return useClientSetting<boolean>(STORAGE_KEY, { defaultValue, parse: parseJsonBoolean })
}
