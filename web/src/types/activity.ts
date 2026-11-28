/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ActivityEventKind } from "./closed-sets.generated"

export interface ActivityEvent {
  kind: ActivityEventKind
  instanceId?: number
  resourceId?: string
  timestamp: string
}

export interface ActivityStreamPayload {
  type: "activity"
  activity?: ActivityEvent
}
