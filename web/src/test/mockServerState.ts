/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ServerState } from "@/types"

/**
 * Build a ServerState object with sensible zero-ish defaults for unit tests.
 *
 * Only override the fields your test actually exercises — the rest carry
 * harmless placeholders so the test file isn't littered with irrelevant
 * boilerplate.
 */
export function makeServerState(overrides: Partial<ServerState> = {}): ServerState {
  return {
    connection_status: "connected",
    dht_nodes: 0,
    dl_info_data: 0,
    dl_info_speed: 0,
    dl_rate_limit: 0,
    up_info_data: 0,
    up_info_speed: 0,
    up_rate_limit: 0,
    queueing: false,
    use_alt_speed_limits: false,
    refresh_interval: 1500,
    ...overrides,
  }
}
