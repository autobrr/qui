/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { FreeSpaceSourceType } from "@/types/automation"

export function usableFreeSpaceSourceType(
  type: FreeSpaceSourceType,
  supportsFreeSpaceAtPath: boolean | undefined
): FreeSpaceSourceType {
  if (type === "qbitPath" && supportsFreeSpaceAtPath === false) {
    return "qbittorrent"
  }
  return type
}
