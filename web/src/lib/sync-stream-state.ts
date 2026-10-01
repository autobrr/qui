/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { StreamState } from "@/contexts/SyncStreamContext"

export function isStreamUsable(state: StreamState): boolean {
  return state.connected && state.initialized && !state.dataStalled && !state.error
}
