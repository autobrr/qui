/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

/**
 * Persists the "Start Torrents Paused" preference per instance.
 *
 * NOTE: This is a workaround for qBittorrent's API limitation where the start_paused_enabled
 * preference cannot be set via app/setPreferences (it gets rejected/ignored). Instead of
 * relying on qBittorrent's global preference, we store this setting ourselves and
 * apply it when adding torrents.
 */
export function usePersistedStartPaused(instanceId: number, defaultValue: boolean = false) {
  return useClientSetting<boolean>(`qui-start-paused-instance-${instanceId}`, {
    defaultValue,
    parse: parseJsonBoolean,
  })
}
