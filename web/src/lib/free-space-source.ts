/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { FreeSpaceSourceType } from "@/types/automation"

/**
 * Returns the free space source a workflow can actually use on this instance.
 * A qBittorrent-host path falls back to the default source only when the instance
 * is known to lack the endpoint; while the capability is still unknown the saved
 * source stands, so opening a workflow never rewrites it by accident.
 */
export function usableFreeSpaceSourceType(
  type: FreeSpaceSourceType,
  supportsFreeSpaceAtPath: boolean | undefined
): FreeSpaceSourceType {
  if (type === "qbitPath" && supportsFreeSpaceAtPath === false) {
    return "qbittorrent"
  }
  return type
}
