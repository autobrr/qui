/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const ALL_INSTANCES_ID = 0

export function isAllInstancesScope(instanceId: number): boolean {
  return instanceId === ALL_INSTANCES_ID
}
